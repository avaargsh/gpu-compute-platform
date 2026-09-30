package agent

import (
	"context"
	"testing"
)

func TestRunnerRestartReplaysDesiredSafely(t *testing.T) {
	control := &fakeControlPlane{desired: []DesiredResource{
		{
			Kind:       "ComputePool",
			ID:         "pool-1",
			Generation: 3,
			Spec: map[string]any{
				"projectID": "project-1",
				"namespace": "project-1",
				"acceleratorBindings": []any{
					map[string]any{"class": "h100", "resourceName": "nvidia.com/gpu", "flavor": "h100"},
				},
			},
		},
		{
			Kind:       "Workload",
			ID:         "train-1",
			Generation: 4,
			Spec: map[string]any{
				"projectID": "project-1",
				"poolID":    "pool-1",
				"namespace": "project-1",
				"image":     "example/train:v4",
				"accelerator": map[string]any{
					"class": "h100",
					"quota": float64(1),
				},
			},
		},
	}}
	runtime := &fakeRuntime{}

	first := NewRunner("cluster-a", control, runtime)
	if err := first.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstProjection := runtime.workload
	if runtime.poolCalls != 1 || runtime.workloadCalls != 1 {
		t.Fatalf("first reconcile calls: pool=%d workload=%d", runtime.poolCalls, runtime.workloadCalls)
	}

	restarted := NewRunner("cluster-a", control, runtime)
	if err := restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.poolCalls != 2 || runtime.workloadCalls != 2 {
		t.Fatalf("restart must replay desired state safely: pool=%d workload=%d", runtime.poolCalls, runtime.workloadCalls)
	}
	if runtime.workload.WorkloadID != firstProjection.WorkloadID ||
		runtime.workload.Generation != firstProjection.Generation ||
		runtime.workload.Image != firstProjection.Image ||
		runtime.workload.AcceleratorBinding.ResourceName != firstProjection.AcceleratorBinding.ResourceName {
		t.Fatalf("restart changed provider projection: before=%#v after=%#v", firstProjection, runtime.workload)
	}
	if len(control.desired) != 2 {
		t.Fatalf("restart must not mutate desired state: %#v", control.desired)
	}
	if len(control.reported) != 4 {
		t.Fatalf("restart must replay observations for both resources: %#v", control.reported)
	}
}
