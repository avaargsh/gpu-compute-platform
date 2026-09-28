package httpapi

import (
	"context"
	"fmt"
	"sync"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Placement struct {
	ClusterID domain.ID
	Namespace string
	Provider  string
}

type PlacementResolver interface {
	ResolveProject(context.Context, domain.ID) (Placement, error)
	ResolvePool(context.Context, domain.ID) (Placement, error)
}

type MemoryPlacementResolver struct {
	mu       sync.RWMutex
	projects map[domain.ID]Placement
	pools    map[domain.ID]Placement
}

func NewMemoryPlacementResolver() *MemoryPlacementResolver {
	return &MemoryPlacementResolver{
		projects: make(map[domain.ID]Placement),
		pools:    make(map[domain.ID]Placement),
	}
}

func (r *MemoryPlacementResolver) BindProject(in domain.ProjectBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[in.ProjectID] = Placement{ClusterID: in.ClusterID, Namespace: in.Namespace}
}

func (r *MemoryPlacementResolver) BindPool(in domain.ClusterBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.pools[in.PoolID]
	current.ClusterID = in.ClusterID
	current.Provider = in.Provider
	r.pools[in.PoolID] = current
}

func (r *MemoryPlacementResolver) ResolveProject(_ context.Context, projectID domain.ID) (Placement, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out, ok := r.projects[projectID]
	if !ok {
		return Placement{}, fmt.Errorf("project binding not found: %s", projectID)
	}
	return out, nil
}

func (r *MemoryPlacementResolver) ResolvePool(_ context.Context, poolID domain.ID) (Placement, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out, ok := r.pools[poolID]
	if !ok {
		return Placement{}, fmt.Errorf("cluster binding not found: %s", poolID)
	}
	return out, nil
}
