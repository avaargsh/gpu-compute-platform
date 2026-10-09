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

func TestVolcanoPodReadbackRefusesStaleListEvidence(t *testing.T) {
	tests := []struct {
		name   string
		change func(*unstructured.Unstructured)
		reason string
	}{
		{"recreated same name, different UID", func(p *unstructured.Unstructured) {
			p.SetUID(types.UID("foreign-recreated-pod"))
		}, "VolcanoPodReadbackDrift"},
		{"resourceVersion changed after LIST", func(p *unstructured.Unstructured) {
			p.SetResourceVersion("32")
		}, "VolcanoPodReadbackDrift"},
		{"Pod became unready", func(p *unstructured.Unstructured) {
			p.Object["status"].(map[string]any)["conditions"] = []any{
				map[string]any{"type": "Ready", "status": "False"},
			}
		}, "VolcanoPodReadbackDrift"},
		{"controller changed", func(p *unstructured.Unstructured) {
			owners := p.GetOwnerReferences()
			owners[0].UID = types.UID("foreign-job")
			p.SetOwnerReferences(owners)
		}, "VolcanoPodReadbackDrift"},
		{"Pod began terminating", func(p *unstructured.Unstructured) {
			now := metav1.Now()
			p.SetDeletionTimestamp(&now)
		}, "VolcanoPodReadbackDrift"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, pod := jobAndPodFixture(t)
			client := fakeJobPodClient(job, pod)
			client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
				fresh := pod.DeepCopy()
				tt.change(fresh)
				return true, fresh, nil
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
			if observed.Phase != "Running" || len(observed.Conditions) != 2 ||
				observed.Conditions[0].Type != "Ready" ||
				observed.Conditions[0].Status != "False" ||
				observed.Conditions[1].Type != "PodsReady" ||
				observed.Conditions[1].Status != "Unknown" ||
				observed.Conditions[1].Reason != tt.reason {
				t.Fatalf("stale LIST/GET evidence cannot prove PodsReady: %#v", observed)
			}
		})
	}
}

func TestVolcanoPodReadbackNotFoundDoesNotPromoteReady(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	client := fakeJobPodClient(job, pod)
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(
			schema.GroupResource{Resource: "pods"}, pod.GetName(),
		)
	})
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := provider.ReconcileWorkload(
		context.Background(), adoptionWorkloadProjection(),
	)
	if err != nil || len(observed.Conditions) != 2 ||
		observed.Conditions[0].Status != "False" ||
		observed.Conditions[1].Status != "Unknown" ||
		observed.Conditions[1].Reason != "VolcanoPodReadbackNotFound" {
		t.Fatalf("Pod disappeared after LIST: observed=%#v err=%v", observed, err)
	}
}

func TestVolcanoPodReadbackTimeoutIsRetryable(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	client := fakeJobPodClient(job, pod)
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := provider.ReconcileWorkload(
		context.Background(), adoptionWorkloadProjection(),
	)
	if err == nil || !baseprovider.IsRetryable(err) ||
		observed.ObservedGeneration != 0 {
		t.Fatalf("failed Pod readback must fail closed and retry from GET: observed=%#v err=%v", observed, err)
	}
}

func TestVolcanoPodReadbackRequiresServerResourceVersion(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	pod.SetResourceVersion("")
	client := fakeJobPodClient(job, pod)
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := provider.ReconcileWorkload(
		context.Background(), adoptionWorkloadProjection(),
	)
	if err != nil || len(observed.Conditions) != 2 ||
		observed.Conditions[1].Status != "Unknown" ||
		observed.Conditions[1].Reason != "VolcanoPodResourceVersionUnavailable" {
		t.Fatalf("unversioned Pod snapshot cannot prove readiness: observed=%#v err=%v", observed, err)
	}
	for _, action := range client.Actions() {
		if action.Matches("get", "pods") {
			t.Fatalf("unversioned LIST must not trigger a success-claim GET: %#v", action)
		}
	}
}
