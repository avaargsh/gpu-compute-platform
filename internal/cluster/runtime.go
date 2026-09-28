package cluster

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
	"github.com/avaargsh/gpu-compute-platform/internal/provider/kueue"
)

type Runtime struct {
	Kueue *kueue.Provider
}

func NewRuntime(clients *Clients) (*Runtime, error) {
	if clients == nil || clients.Core == nil || clients.Dynamic == nil {
		return nil, fmt.Errorf("cluster clients are required")
	}
	kubeClient := kueue.NewKubeClient(clients.Core, clients.Dynamic)
	return &Runtime{
		Kueue: kueue.NewProvider(kubeClient),
	}, nil
}

func (r *Runtime) ReconcilePool(ctx context.Context, in provider.PoolProjection) (provider.PoolObservation, error) {
	if r == nil || r.Kueue == nil {
		return provider.PoolObservation{}, fmt.Errorf("kueue provider is unavailable")
	}
	return r.Kueue.ReconcilePool(ctx, in)
}

func (r *Runtime) ReconcileWorkload(ctx context.Context, in provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	if r == nil || r.Kueue == nil {
		return provider.WorkloadObservation{}, fmt.Errorf("kueue provider is unavailable")
	}
	return r.Kueue.ReconcileWorkload(ctx, in)
}

func (r *Runtime) DeletePool(ctx context.Context, in provider.PoolProjection) (provider.DeletionObservation, error) {
	if r == nil || r.Kueue == nil {
		return provider.DeletionObservation{}, fmt.Errorf("kueue provider is unavailable")
	}
	return r.Kueue.DeletePool(ctx, in)
}

func (r *Runtime) DeleteWorkload(ctx context.Context, in provider.WorkloadProjection) (provider.DeletionObservation, error) {
	if r == nil || r.Kueue == nil {
		return provider.DeletionObservation{}, fmt.Errorf("kueue provider is unavailable")
	}
	return r.Kueue.DeleteWorkload(ctx, in)
}
