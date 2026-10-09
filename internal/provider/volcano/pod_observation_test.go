package volcano

import (
	"context"
	"testing"

	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func jobAndPodFixture(t *testing.T) (*unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	job, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	job.SetUID(types.UID("job-uid-1"))
	job.SetResourceVersion("21")
	job.Object["status"] = map[string]any{
		"state": map[string]any{"phase": "Running"},
	}
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"name":      job.GetName() + "-workload-0",
			"namespace": job.GetNamespace(),
			"uid":       "pod-uid-1",
			"labels": map[string]any{
				"volcano.sh/job-name":      job.GetName(),
				"volcano.sh/job-namespace": job.GetNamespace(),
				"volcano.sh/task-spec":     "workload",
			},
		},
		"spec": map[string]any{"nodeName": "node-a"},
		"status": map[string]any{
			"phase":      "Running",
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}}
	controller := true
	pod.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "batch.volcano.sh/v1alpha1", Kind: "Job",
		Name: job.GetName(), UID: job.GetUID(), Controller: &controller,
	}})
	return job, pod
}

func fakeJobPodClient(job *unstructured.Unstructured, pods ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	objects := []runtime.Object{job}
	for _, pod := range pods {
		objects = append(objects, pod)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			volcanoQueueGVR: "QueueList",
			volcanoJobGVR:   "JobList",
			volcanoPodGVR:   "PodList",
		},
		objects...,
	)
}

func TestVolcanoRunningJobAddsUIDBoundPodReadinessWithoutReadyPromotion(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	client := fakeJobPodClient(job, pod)
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := provider.ReconcileWorkload(context.Background(), adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	if observation.Phase != "Running" || len(observation.Conditions) != 2 ||
		observation.Conditions[0].Type != "Ready" ||
		observation.Conditions[0].Status != "False" ||
		observation.Conditions[1].Type != "PodsReady" ||
		observation.Conditions[1].Status != "True" ||
		observation.Conditions[1].Reason != "VolcanoOwnedPodReady" {
		t.Fatalf("UID-bound Pod is observed, not promoted to scheduler Ready: %#v", observation)
	}
	podLists := 0
	for _, action := range client.Actions() {
		if action.Matches("list", "pods") {
			podLists++
			listAction, ok := action.(k8stesting.ListAction)
			expectedSelector := labels.Set{
				"volcano.sh/job-name": job.GetName(),
			}.AsSelector().String()
			if !ok || action.GetNamespace() != job.GetNamespace() ||
				listAction.GetListRestrictions().Labels == nil ||
				listAction.GetListRestrictions().Labels.String() != expectedSelector {
				t.Fatalf("Pod LIST must use exact namespace/job-name selector: %#v", action)
			}
		}
		if action.Matches("create", "pods") || action.Matches("update", "pods") ||
			action.Matches("delete", "pods") {
			t.Fatalf("Pod evidence must remain strictly read-only: %#v", action)
		}
	}
	if podLists != 1 {
		t.Fatalf("Pod LIST calls=%d, want 1", podLists)
	}
}

func TestVolcanoPodReadinessNegativeControls(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*unstructured.Unstructured, *unstructured.Unstructured)
		want   string
	}{
		{"foreign UID", func(_, p *unstructured.Unstructured) {
			owners := p.GetOwnerReferences()
			owners[0].UID = types.UID("other-job")
			p.SetOwnerReferences(owners)
		}, "Unknown"},
		{"not controller", func(_, p *unstructured.Unstructured) {
			owners := p.GetOwnerReferences()
			f := false
			owners[0].Controller = &f
			p.SetOwnerReferences(owners)
		}, "Unknown"},
		{"foreign task", func(_, p *unstructured.Unstructured) {
			l := p.GetLabels()
			l["volcano.sh/task-spec"] = "other"
			p.SetLabels(l)
		}, "Unknown"},
		{"missing pod UID", func(_, p *unstructured.Unstructured) {
			p.SetUID("")
		}, "Unknown"},
		{"not scheduled", func(_, p *unstructured.Unstructured) {
			p.Object["spec"].(map[string]any)["nodeName"] = ""
		}, "False"},
		{"not ready", func(_, p *unstructured.Unstructured) {
			p.Object["status"].(map[string]any)["conditions"] = []any{
				map[string]any{"type": "Ready", "status": "False"},
			}
		}, "False"},
		{"succeeded not running", func(_, p *unstructured.Unstructured) {
			p.Object["status"].(map[string]any)["phase"] = "Succeeded"
		}, "False"},
		{"no job UID", func(j, _ *unstructured.Unstructured) {
			j.SetUID("")
		}, "Unknown"},
		{"unreviewed task shape", func(j, _ *unstructured.Unstructured) {
			tasks := j.Object["spec"].(map[string]any)["tasks"].([]any)
			tasks[0].(map[string]any)["replicas"] = int64(2)
		}, "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, pod := jobAndPodFixture(t)
			tt.mutate(job, pod)
			client := fakeJobPodClient(job, pod)
			provider, err := NewProvider(client)
			if err != nil {
				t.Fatal(err)
			}
			observation, err := provider.ReconcileWorkload(
				context.Background(), adoptionWorkloadProjection(),
			)
			// An unreviewed Job task projection is rejected at the stronger
			// existing immutable-spec admission boundary.
			if tt.name == "unreviewed task shape" {
				if err == nil {
					t.Fatal("Job task spec drift must fail admission")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if observation.Conditions[0].Status != "False" ||
				len(observation.Conditions) != 2 ||
				observation.Conditions[1].Status != tt.want {
				t.Fatalf("negative Pod readiness control failed: %#v", observation)
			}
		})
	}
}

func TestVolcanoRunningJobWithoutPodsDoesNotClaimReadiness(t *testing.T) {
	job, _ := jobAndPodFixture(t)
	client := fakeJobPodClient(job)
	p, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := p.ReconcileWorkload(context.Background(), adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Conditions) != 2 || observed.Conditions[1].Status != "Unknown" ||
		observed.Conditions[1].Reason != "VolcanoPodCountUnproven" {
		t.Fatalf("zero Pods cannot prove readiness: %#v", observed.Conditions)
	}
}

func TestVolcanoPodListFailureIsRetryableNotReady(t *testing.T) {
	job, pod := jobAndPodFixture(t)
	client := fakeJobPodClient(job, pod)
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	p, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := p.ReconcileWorkload(context.Background(), adoptionWorkloadProjection())
	if err == nil || observed.ObservedGeneration != 0 ||
		!baseprovider.IsRetryable(err) {
		t.Fatalf("failed Pod LIST must fail-closed and retry: %#v err=%v", observed, err)
	}
}

func TestPodReadinessRejectsDuplicateReadyConditions(t *testing.T) {
	_, pod := jobAndPodFixture(t)
	pod.Object["status"].(map[string]any)["conditions"] = []any{
		map[string]any{"type": "Ready", "status": "True"},
		map[string]any{"type": "Ready", "status": "True"},
	}
	if podKubernetesReady(pod) {
		t.Fatal("duplicate Ready conditions must not provide positive evidence")
	}
}
