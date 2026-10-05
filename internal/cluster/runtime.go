package cluster

import (
	"context"
	"fmt"
	"sort"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
	"github.com/avaargsh/gpu-compute-platform/internal/provider/kueue"
)

type Runtime struct {
	adapters map[string]provider.Adapter
}

func NewRuntime(clients *Clients) (*Runtime, error) {
	if clients == nil || clients.Core == nil || clients.Dynamic == nil {
		return nil, fmt.Errorf("cluster clients are required")
	}
	kubeClient := kueue.NewKubeClient(clients.Core, clients.Dynamic)
	return NewRuntimeWithAdapters(map[string]provider.Adapter{
		provider.KueueAdapterName: kueue.NewProvider(kubeClient),
	})
}

// NewRuntimeWithAdapters is the Stage B provider SPI.
//
// Registration is explicit and local to the Cluster Agent. The management
// plane binds a pool to one immutable provider identity; the Runtime only
// dispatches that already-authorized identity. It does not choose a scheduler
// or placement policy.
func NewRuntimeWithAdapters(adapters map[string]provider.Adapter) (*Runtime, error) {
	if len(adapters) == 0 {
		return nil, fmt.Errorf("at least one provider adapter is required")
	}
	frozen := make(map[string]provider.Adapter, len(adapters))
	for name, adapter := range adapters {
		if name == "" {
			return nil, fmt.Errorf("provider adapter name is required")
		}
		if adapter == nil {
			return nil, fmt.Errorf("provider adapter %q is nil", name)
		}
		if _, exists := frozen[name]; exists {
			return nil, fmt.Errorf("duplicate provider adapter %q", name)
		}
		frozen[name] = adapter
	}
	return &Runtime{adapters: frozen}, nil
}

func (r *Runtime) ProviderNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) adapter(name string) (provider.Adapter, error) {
	if r == nil {
		return nil, fmt.Errorf("cluster runtime is required")
	}
	if name == "" {
		return nil, fmt.Errorf("provider identity is required")
	}
	adapter, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("provider adapter is not registered: %s", name)
	}
	return adapter, nil
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
