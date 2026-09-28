package agent

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type fakeControlPlane struct {
	desired  []DesiredResource
	reported []Observation
}

func (f *fakeControlPlane) Register(context.Context, Registration) error { return nil }
func (f *fakeControlPlane) Heartbeat(context.Context, Heartbeat) error   { return nil }
func (f *fakeControlPlane) PullDesired(context.Context, domain.ID) ([]DesiredResource, error) {
	return f.desired, nil
}
func (f *fakeControlPlane) Report(_ context.Context, _ domain.ID, observations []Observation) error {
	f.reported = append(f.reported, observations...)
	return nil
}

type fakeRuntime struct{}

func (fakeRuntime) ReconcilePool(_ context.Context, p provider.PoolProjection) (provider.PoolObservation, error) {
	return provider.PoolObservation{ObservedGeneration: p.Generation, EvidenceRefs: []string{"pool-evidence"}}, nil
}

func (fakeRuntime) ReconcileWorkload(_ context.Context, p provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	return provider.WorkloadObservation{ObservedGeneration: p.Generation, Phase: "Running", EvidenceRefs: []string{"job-evidence"}}, nil
}

func TestRunnerPullsReconcilesAndReports(t *testing.T) {
	control := &fakeControlPlane{
		desired: []DesiredResource{
			{
				Kind:       "ComputePool",
				ID:         "pool-1",
				Generation: 3,
				Spec: map[string]any{
					"projectID":  "project-1",
					"namespace":  "project-1",
					"accelerators": []any{map[string]any{"class": "h100-80g", "quota": float64(8)}},
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
					"queueName": "lq-pool-1",
					"image":     "example/train:latest",
					"accelerator": map[string]any{"class": "h100-80g", "quota": float64(2)},
				},
			},
		},
	}

	runner := NewRunner("cluster-a", control, fakeRuntime{})
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.reported) != 2 {
		t.Fatalf("reported=%d, want 2", len(control.reported))
	}
	if control.reported[0].ObservedGeneration != 3 || control.reported[1].ObservedGeneration != 4 {
		t.Fatalf("unexpected generations: %#v", control.reported)
	}
	if control.reported[1].EvidenceRefs[0] != "job-evidence" {
		t.Fatalf("missing workload evidence: %#v", control.reported[1])
	}
}
