package volcano

import (
	"context"
	"testing"

	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"
)

func jobPodGroupFixture(t *testing.T, job *unstructured.Unstructured) *unstructured.Unstructured {
	t.Helper()
	queue, found, err := unstructured.NestedString(job.Object, "spec", "queue")
	if err != nil || !found {
		t.Fatalf("test Job queue: found=%t err=%v", found, err)
	}
	pg := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "scheduling.volcano.sh/v1beta1",
		"kind":       "PodGroup",
		"metadata": map[string]any{
			"name":      job.GetName() + "-" + string(job.GetUID()),
			"namespace": job.GetNamespace(),
			"uid":       "pg-uid-1",
		},
		"spec": map[string]any{
			"queue": queue, "minMember": int64(1),
		},
		"status": map[string]any{"phase": "Running"},
	}}
	controller := true
	pg.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: job.GetAPIVersion(), Kind: job.GetKind(),
		Name: job.GetName(), UID: job.GetUID(), Controller: &controller,
	}})
	pg.SetResourceVersion("41")
	return pg
}

func TestVolcanoPodGroupLinkIsIndependentAndNeverPromotesReady(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	pg := jobPodGroupFixture(t, job)
	client := fakeJobPodClient(job, pod, pg)
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := provider.ReconcileWorkload(
		context.Background(), adoptionWorkloadProjection(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Phase != "Running" || len(observed.Conditions) != 3 ||
		observed.Conditions[0].Type != "Ready" ||
		observed.Conditions[0].Status != "False" ||
		observed.Conditions[1].Type != "PodsReady" ||
		observed.Conditions[1].Status != "True" ||
		observed.Conditions[2].Type != "PodGroupLinked" ||
		observed.Conditions[2].Status != "True" ||
		observed.Conditions[2].Reason != "VolcanoPodGroupControllerLinkVerified" {
		t.Fatalf("PodGroup controller link must not imply scheduler Ready: %#v", observed)
	}
	reads := 0
	for _, action := range client.Actions() {
		if action.Matches("get", "podgroups") {
			reads++
			g, ok := action.(k8stesting.GetAction)
			if !ok || action.GetNamespace() != job.GetNamespace() ||
				g.GetName() != pg.GetName() {
				t.Fatalf("PodGroup GET was not restricted to the Job UID name: %#v", action)
			}
		}
		if action.Matches("create", "podgroups") ||
			action.Matches("update", "podgroups") ||
			action.Matches("delete", "podgroups") {
			t.Fatalf("PodGroup evidence must not mutate: %#v", action)
		}
	}
	if reads != 2 {
		t.Fatalf("expected two independent PodGroup GETs, got %d", reads)
	}
}

func TestVolcanoPodGroupLinkFalsification(t *testing.T) {
	tests := []struct {
		name   string
		change func(*unstructured.Unstructured)
	}{
		{"wrong owner UID", func(pg *unstructured.Unstructured) {
			owners := pg.GetOwnerReferences()
			owners[0].UID = types.UID("another-job")
			pg.SetOwnerReferences(owners)
		}},
		{"not a controller", func(pg *unstructured.Unstructured) {
			owners := pg.GetOwnerReferences()
			v := false
			owners[0].Controller = &v
			pg.SetOwnerReferences(owners)
		}},
		{"wrong queue", func(pg *unstructured.Unstructured) {
			pg.Object["spec"].(map[string]any)["queue"] = "foreign-queue"
		}},
		{"wrong minMember", func(pg *unstructured.Unstructured) {
			pg.Object["spec"].(map[string]any)["minMember"] = int64(2)
		}},
		{"missing UID", func(pg *unstructured.Unstructured) { pg.SetUID("") }},
		{"missing resourceVersion", func(pg *unstructured.Unstructured) { pg.SetResourceVersion("") }},
		{"deleting", func(pg *unstructured.Unstructured) {
			now := metav1.Now()
			pg.SetDeletionTimestamp(&now)
		}},
		{"different namespace", func(pg *unstructured.Unstructured) {
			pg.SetNamespace("foreign-namespace")
		}},
		{"wrong group kind", func(pg *unstructured.Unstructured) { pg.SetKind("Queue") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, pod := jobAndPodFixture(t)
			pg := jobPodGroupFixture(t, job)
			tt.change(pg)
			client := fakeJobPodClient(job, pod)
			client.PrependReactor("get", "podgroups", func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, pg.DeepCopy(), nil
			})
			provider, err := NewProvider(client)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := provider.ReconcileWorkload(
				context.Background(), adoptionWorkloadProjection(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(observed.Conditions) != 3 ||
				observed.Conditions[0].Status != "False" ||
				observed.Conditions[1].Status != "True" ||
				observed.Conditions[2].Type != "PodGroupLinked" ||
				observed.Conditions[2].Status != "Unknown" ||
				observed.Conditions[2].Reason != "VolcanoPodGroupOwnershipUnproven" {
				t.Fatalf("untrusted PodGroup must be rejected: %#v", observed)
			}
		})
	}
}

func TestVolcanoPodGroupLinkCannotPromoteReplacedReadback(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*unstructured.Unstructured)
	}{
		{"recreated same name", func(pg *unstructured.Unstructured) {
			pg.SetUID(types.UID("replaced-pg"))
		}},
		{"resourceVersion changed", func(pg *unstructured.Unstructured) {
			pg.SetResourceVersion("42")
		}},
		{"controller changed", func(pg *unstructured.Unstructured) {
			owners := pg.GetOwnerReferences()
			owners[0].UID = types.UID("different-job")
			pg.SetOwnerReferences(owners)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			job, pod := jobAndPodFixture(t)
			pg := jobPodGroupFixture(t, job)
			client := fakeJobPodClient(job, pod, pg)
			gets := 0
			client.PrependReactor("get", "podgroups", func(action k8stesting.Action) (bool, runtime.Object, error) {
				gets++
				p := pg.DeepCopy()
				if gets == 2 {
					tt.change(p)
				}
				return true, p, nil
			})
			provider, err := NewProvider(client)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := provider.ReconcileWorkload(
				context.Background(), adoptionWorkloadProjection(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if gets != 2 || len(observed.Conditions) != 3 ||
				observed.Conditions[2].Status != "Unknown" ||
				observed.Conditions[2].Reason != "VolcanoPodGroupReadbackDrift" {
				t.Fatalf("changed PodGroup cannot be promoted: %#v reads=%d", observed, gets)
			}
		})
	}
}

func TestVolcanoPodGroupLinkNotFoundAndTimeout(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	provider, err := NewProvider(fakeJobPodClient(job, pod))
	if err != nil {
		t.Fatal(err)
	}
	observed, err := provider.ReconcileWorkload(context.Background(), adoptionWorkloadProjection())
	if err != nil || len(observed.Conditions) != 3 ||
		observed.Conditions[2].Status != "Unknown" ||
		observed.Conditions[2].Reason != "VolcanoPodGroupNotFound" {
		t.Fatalf("missing PodGroup must be unknown, not admitted: %#v err=%v", observed, err)
	}

	client := fakeJobPodClient(job, pod)
	client.PrependReactor("get", "podgroups", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	provider, err = NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err = provider.ReconcileWorkload(context.Background(), adoptionWorkloadProjection())
	if err == nil || !baseprovider.IsRetryable(err) ||
		observed.ObservedGeneration != 0 {
		t.Fatalf("transient PodGroup readback must fail closed: %#v err=%v", observed, err)
	}
}

func TestVolcanoPodGroupSecondReadNotFoundFailsClosed(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	pg := jobPodGroupFixture(t, job)
	client := fakeJobPodClient(job, pod, pg)
	n := 0
	client.PrependReactor("get", "podgroups", func(action k8stesting.Action) (bool, runtime.Object, error) {
		n++
		if n == 2 {
			return true, nil, apierrors.NewNotFound(
				schema.GroupResource{Group: volcanoPodGroupGVR.Group, Resource: "podgroups"}, pg.GetName(),
			)
		}
		return true, pg.DeepCopy(), nil
	})
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := provider.ReconcileWorkload(context.Background(), adoptionWorkloadProjection())
	if err != nil || n != 2 || len(observed.Conditions) != 3 ||
		observed.Conditions[2].Status != "Unknown" ||
		observed.Conditions[2].Reason != "VolcanoPodGroupReadbackNotFound" {
		t.Fatalf("PodGroup disappeared after first GET: %#v err=%v", observed, err)
	}
}
