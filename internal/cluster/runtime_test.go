package cluster

import (
	"context"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type recordingAdapter struct {
	poolReconcileCalls     int
	workloadReconcileCalls int
	poolDeleteCalls        int
	workloadDeleteCalls    int
}

func (a *recordingAdapter) ReconcilePool(_ context.Context, in provider.PoolProjection) (provider.PoolObservation, error) {
	a.poolReconcileCalls++
	return provider.PoolObservation{ObservedGeneration: in.Generation}, nil
}

func (a *recordingAdapter) DeletePool(_ context.Context, _ provider.PoolProjection) (provider.DeletionObservation, error) {
	a.poolDeleteCalls++
	return provider.DeletionObservation{Gone: true}, nil
}

func (a *recordingAdapter) ReconcileWorkload(_ context.Context, in provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	a.workloadReconcileCalls++
	return provider.WorkloadObservation{ObservedGeneration: in.Generation}, nil
}

func (a *recordingAdapter) DeleteWorkload(_ context.Context, _ provider.WorkloadProjection) (provider.DeletionObservation, error) {
	a.workloadDeleteCalls++
	return provider.DeletionObservation{Gone: true}, nil
}

func TestRuntimeDispatchesFrozenProviderIdentity(t *testing.T) {
	kueueAdapter := &recordingAdapter{}
	otherAdapter := &recordingAdapter{}
	runtime, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue": kueueAdapter,
		"test-only": otherAdapter,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.ReconcilePool(context.Background(), provider.PoolProjection{
		Provider: "kueue",
		Generation: 7,
	}); err != nil {
		t.Fatal(err)
	}
	if kueueAdapter.poolReconcileCalls != 1 || otherAdapter.poolReconcileCalls != 0 {
		t.Fatalf(
			"provider dispatch crossed identity: kueue=%d other=%d",
			kueueAdapter.poolReconcileCalls,
			otherAdapter.poolReconcileCalls,
		)
	}

	if _, err := runtime.ReconcileWorkload(context.Background(), provider.WorkloadProjection{
		Provider: "test-only",
		Generation: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if otherAdapter.workloadReconcileCalls != 1 || kueueAdapter.workloadReconcileCalls != 0 {
		t.Fatalf(
			"workload dispatch crossed identity: kueue=%d other=%d",
			kueueAdapter.workloadReconcileCalls,
			otherAdapter.workloadReconcileCalls,
		)
	}
}

func TestRuntimeFailsClosedForMissingOrUnknownProvider(t *testing.T) {
	runtime, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue": &recordingAdapter{},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"", "volcano"} {
		_, err := runtime.ReconcilePool(context.Background(), provider.PoolProjection{
			Provider: name,
			Generation: 1,
		})
		if err == nil {
			t.Fatalf("provider %q unexpectedly dispatched", name)
		}
		if !strings.Contains(err.Error(), "provider") {
			t.Fatalf("provider %q error=%v", name, err)
		}
	}
}

func TestRuntimeRequiresExplicitNonNilAdapters(t *testing.T) {
	if _, err := NewRuntimeWithAdapters(nil); err == nil {
		t.Fatal("empty provider registry must fail closed")
	}
	if _, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"": &recordingAdapter{},
	}); err == nil {
		t.Fatal("empty provider name must fail closed")
	}
	if _, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue": nil,
	}); err == nil {
		t.Fatal("nil provider adapter must fail closed")
	}
}
