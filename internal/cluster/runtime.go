package cluster

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
	"github.com/avaargsh/gpu-compute-platform/internal/provider/kueue"
)

const legacyDefaultProvider = "kueue"

type Runtime struct {
	providers map[string]provider.Adapter
}

func NewRuntime(clients *Clients) (*Runtime, error) {
	if clients == nil || clients.Core == nil || clients.Dynamic == nil {
		return nil, fmt.Errorf("cluster clients are required")
	}
	kubeClient := kueue.NewKubeClient(clients.Core, clients.Dynamic)
	return newRuntimeWithProviders(map[string]provider.Adapter{
		"kueue": kueue.NewProvider(kubeClient),
	}), nil
}

func newRuntimeWithProviders(providers map[string]provider.Adapter) *Runtime {
	copied := make(map[string]provider.Adapter, len(providers))
	for name, adapter := range providers {
		copied[name] = adapter
	}
	return &Runtime{providers: copied}
}

func (r *Runtime) ReconcilePool(ctx context.Context, in provider.PoolProjection) (provider.PoolObservation, error) {
	adapter, err := r.adapter(in.Provider)
	if err != nil {
		return provider.PoolObservation{}, err
	}
	return adapter.ReconcilePool(ctx, in)
}

func (r *Runtime) ReconcileWorkload(ctx context.Context, in provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	adapter, err := r.adapter(in.Provider)
	if err != nil {
		return provider.WorkloadObservation{}, err
	}
	return adapter.ReconcileWorkload(ctx, in)
}

func (r *Runtime) DeletePool(ctx context.Context, in provider.PoolProjection) (provider.DeletionObservation, error) {
	adapter, err := r.adapter(in.Provider)
	if err != nil {
		return provider.DeletionObservation{}, err
	}
	return adapter.DeletePool(ctx, in)
}

func (r *Runtime) DeleteWorkload(ctx context.Context, in provider.WorkloadProjection) (provider.DeletionObservation, error) {
	adapter, err := r.adapter(in.Provider)
	if err != nil {
		return provider.DeletionObservation{}, err
	}
	return adapter.DeleteWorkload(ctx, in)
}

func (r *Runtime) adapter(name string) (provider.Adapter, error) {
	if r == nil {
		return nil, fmt.Errorf("provider runtime is unavailable")
	}
	if name == "" {
		// v0.1 desired objects predate provider identity in projections. Preserve
		// their deterministic recovery path without making the default mutable.
		name = legacyDefaultProvider
	}
	adapter, ok := r.providers[name]
	if !ok || adapter == nil {
		return nil, fmt.Errorf("provider %q is unavailable", name)
	}
	return adapter, nil
}
