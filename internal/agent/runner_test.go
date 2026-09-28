package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type fakeControlPlane struct {
	desired      []DesiredResource
	reported     []Observation
	leaseClaimed bool
	denyLease    bool
	claimCalls   int
	releaseCalls int
	reportBeforeRelease bool
}

func (f *fakeControlPlane) Register(context.Context, Registration) error { return nil }
func (f *fakeControlPlane) Heartbeat(context.Context, Heartbeat) error   { return nil }
func (f *fakeControlPlane) PullDesired(context.Context, domain.ID) ([]DesiredResource, error) {
	return f.desired, nil
}
func (f *fakeControlPlane) Report(_ context.Context, _ domain.ID, observations []Observation) error {
	if f.releaseCalls == 0 {
		f.reportBeforeRelease = true
	}
	f.reported = append(f.reported, observations...)
	return nil
}
func (f *fakeControlPlane) ClaimReconcileLease(_ context.Context, _ ReconcileLeaseRequest) (bool, error) {
	f.claimCalls++
	if f.denyLease {
		return false, nil
	}
	return true, nil
}
func (f *fakeControlPlane) ReleaseReconcileLease(_ context.Context, _ ReconcileLeaseRequest) error {
	f.releaseCalls++
	return nil
}

type fakeRuntime struct {
	poolCalls     int
	workload      provider.WorkloadProjection
	workloadErr   error
	workloadCalls int
}

func (f *fakeRuntime) ReconcilePool(_ context.Context, p provider.PoolProjection) (provider.PoolObservation, error) {
	f.poolCalls++
	return provider.PoolObservation{ObservedGeneration: p.Generation, EvidenceRefs: []string{"pool-evidence"}}, nil
}

func (f *fakeRuntime) ReconcileWorkload(_ context.Context, p provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	f.workload = p
	f.workloadCalls++
	if f.workloadErr != nil {
		return provider.WorkloadObservation{}, f.workloadErr
	}
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
					"projectID": "project-1",
					"namespace": "project-1",
					"accelerators": []any{
						map[string]any{
							"class": "h100-80g",
							"quota": float64(8),
						},
					},
					"acceleratorBindings": []any{
						map[string]any{
							"class":        "h100-80g",
							"resourceName": "vendor.example/gpu",
							"flavor":       "h100",
						},
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
					"image":     "example/train:latest",
					"accelerator": map[string]any{
						"class": "h100-80g",
						"quota": float64(2),
					},
				},
			},
		},
	}

	runtime := &fakeRuntime{}
	runner := NewRunner("cluster-a", control, runtime)
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !control.reportBeforeRelease {
		t.Fatal("observation must be reported before reconcile lease release")
	}
	if len(control.reported) != 2 {
		t.Fatalf("reported=%d, want 2", len(control.reported))
	}
	if control.reported[0].ObservedGeneration != 3 || control.reported[1].ObservedGeneration != 4 {
		t.Fatalf("unexpected generations: %#v", control.reported)
	}
	if runtime.workload.AcceleratorBinding.ResourceName != "vendor.example/gpu" {
		t.Fatalf("workload binding was not resolved: %#v", runtime.workload.AcceleratorBinding)
	}
	if control.reported[1].EvidenceRefs[0] != "job-evidence" {
		t.Fatalf("missing workload evidence: %#v", control.reported[1])
	}
}

func TestRunnerBacksOffRetryableFailureAndResetsOnNewGeneration(t *testing.T) {
	control := &fakeControlPlane{desired: []DesiredResource{
		{Kind: "ComputePool", ID: "pool-1", Generation: 1, Spec: map[string]any{
			"acceleratorBindings": []any{map[string]any{"class": "h100", "resourceName": "nvidia.com/gpu", "flavor": "h100"}},
		}},
		{Kind: "Workload", ID: "train-1", Generation: 1, Spec: map[string]any{
			"poolID": "pool-1", "accelerator": map[string]any{"class": "h100", "quota": float64(1)},
		}},
	}}
	runtime := &fakeRuntime{workloadErr: provider.MarkRetryable(errors.New("api timeout"))}
	runner := NewRunner("cluster-a", control, runtime)
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }

	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.workloadCalls != 1 {
		t.Fatalf("calls=%d, want 1", runtime.workloadCalls)
	}
	if len(control.reported) != 1 {
		t.Fatalf("retryable workload failure must not be reported as terminal: %#v", control.reported)
	}

	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.workloadCalls != 1 {
		t.Fatalf("backoff must suppress immediate retry, calls=%d", runtime.workloadCalls)
	}

	control.desired[1].Generation = 2
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.workloadCalls != 2 {
		t.Fatalf("new generation must bypass old backoff, calls=%d", runtime.workloadCalls)
	}
}

func TestRetryBackoffIsDeterministicAndCapped(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 32 * time.Second}
	for i, expected := range want {
		if got := retryBackoff(i + 1); got != expected {
			t.Fatalf("attempt %d: got %s want %s", i+1, got, expected)
		}
	}
}

func TestRunnerSkipsProviderWhenLeaseIsContended(t *testing.T) {
	control := &fakeControlPlane{
		denyLease: true,
		desired: []DesiredResource{{
			Kind: "ComputePool", ID: "pool-1", Generation: 1,
			Spec: map[string]any{"acceleratorBindings": []any{}},
		}},
	}
	runtime := &fakeRuntime{}
	runner := NewRunner("cluster-a", control, runtime)

	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if control.claimCalls != 1 {
		t.Fatalf("claim calls=%d, want 1", control.claimCalls)
	}
	if runtime.poolCalls != 0 {
		t.Fatalf("contended lease must suppress provider side effects, pool calls=%d", runtime.poolCalls)
	}
	if control.releaseCalls != 0 {
		t.Fatalf("contended lease must not be released by non-owner, releases=%d", control.releaseCalls)
	}
	if len(control.reported) != 0 {
		t.Fatalf("contended reconcile must not report terminal state: %#v", control.reported)
	}
}
