package cluster

import (
	"context"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type recordingAdapter struct {
	poolReconciles     int
	workloadReconciles int
	poolDeletes        int
	workloadDeletes    int
}

func (a *recordingAdapter) ReconcilePool(context.Context, provider.PoolProjection) (provider.PoolObservation, error) {
	a.poolReconciles++
	return provider.PoolObservation{}, nil
}

func (a *recordingAdapter) DeletePool(context.Context, provider.PoolProjection) (provider.DeletionObservation, error) {
	a.poolDeletes++
	return provider.DeletionObservation{Gone: true}, nil
}

func (a *recordingAdapter) ReconcileWorkload(context.Context, provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	a.workloadReconciles++
	return provider.WorkloadObservation{}, nil
}

func (a *recordingAdapter) DeleteWorkload(context.Context, provider.WorkloadProjection) (provider.DeletionObservation, error) {
	a.workloadDeletes++
	return provider.DeletionObservation{Gone: true}, nil
}

func TestRuntimeRoutesByFrozenProviderIdentity(t *testing.T) {
	kueueAdapter := &recordingAdapter{}
	otherAdapter := &recordingAdapter{}
	runtime := newRuntimeWithProviders(map[string]provider.Adapter{
		"kueue": kueueAdapter,
		"other": otherAdapter,
	})

	if _, err := runtime.ReconcilePool(context.Background(), provider.PoolProjection{Provider: "other"}); err != nil {
		t.Fatal(err)
	}
	if otherAdapter.poolReconciles != 1 || kueueAdapter.poolReconciles != 0 {
		t.Fatalf("unexpected provider routing: kueue=%d other=%d", kueueAdapter.poolReconciles, otherAdapter.poolReconciles)
	}
}

func TestRuntimeLegacyEmptyProviderUsesFrozenV01KueueDefault(t *testing.T) {
	kueueAdapter := &recordingAdapter{}
	runtime := newRuntimeWithProviders(map[string]provider.Adapter{"kueue": kueueAdapter})

	if _, err := runtime.ReconcileWorkload(context.Background(), provider.WorkloadProjection{}); err != nil {
		t.Fatal(err)
	}
	if kueueAdapter.workloadReconciles != 1 {
		t.Fatalf("kueue workload reconciles=%d, want 1", kueueAdapter.workloadReconciles)
	}
}

func TestRuntimeUnknownProviderFailsClosed(t *testing.T) {
	kueueAdapter := &recordingAdapter{}
	runtime := newRuntimeWithProviders(map[string]provider.Adapter{"kueue": kueueAdapter})

	_, err := runtime.DeletePool(context.Background(), provider.PoolProjection{Provider: "volcano"})
	if err == nil || !strings.Contains(err.Error(), `provider "volcano" is unavailable`) {
		t.Fatalf("error=%v, want unavailable provider", err)
	}
	if kueueAdapter.poolDeletes != 0 {
		t.Fatalf("kueue delete called for unknown provider: %d", kueueAdapter.poolDeletes)
	}
}
