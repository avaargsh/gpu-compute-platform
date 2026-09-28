package httpapi

import (
	"context"
	"fmt"
	"sync"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
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

type BindingStore interface {
	PlacementResolver
	UpsertProjectBinding(context.Context, domain.ProjectBinding) error
	UpsertClusterBinding(context.Context, domain.ClusterBinding) error
}

type MemoryPlacementResolver struct {
	mu                 sync.RWMutex
	projects           map[domain.ID]Placement
	pools              map[domain.ID]Placement
	projectGenerations map[domain.ID]int64
	poolGenerations    map[domain.ID]int64
}

func NewMemoryPlacementResolver() *MemoryPlacementResolver {
	return &MemoryPlacementResolver{
		projects:           make(map[domain.ID]Placement),
		pools:              make(map[domain.ID]Placement),
		projectGenerations: make(map[domain.ID]int64),
		poolGenerations:    make(map[domain.ID]int64),
	}
}

func (r *MemoryPlacementResolver) BindProject(in domain.ProjectBinding) {
	_ = r.UpsertProjectBinding(context.Background(), in)
}

func (r *MemoryPlacementResolver) UpsertProjectBinding(_ context.Context, in domain.ProjectBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in.Metadata.Generation < r.projectGenerations[in.ProjectID] {
		return agentstore.ErrStaleGeneration
	}
	current := r.projects[in.ProjectID]
	if current.ClusterID != "" && current.ClusterID != in.ClusterID {
		return agentstore.ErrPlacementMigrationRequired
	}
	current.ClusterID = in.ClusterID
	current.Namespace = in.Namespace
	r.projects[in.ProjectID] = current
	r.projectGenerations[in.ProjectID] = in.Metadata.Generation
	return nil
}

func (r *MemoryPlacementResolver) BindPool(in domain.ClusterBinding) {
	_ = r.UpsertClusterBinding(context.Background(), in)
}

func (r *MemoryPlacementResolver) UpsertClusterBinding(_ context.Context, in domain.ClusterBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in.Metadata.Generation < r.poolGenerations[in.PoolID] {
		return agentstore.ErrStaleGeneration
	}
	current := r.pools[in.PoolID]
	if current.ClusterID != "" && current.ClusterID != in.ClusterID {
		return agentstore.ErrPlacementMigrationRequired
	}
	current.ClusterID = in.ClusterID
	current.Provider = in.Provider
	r.pools[in.PoolID] = current
	r.poolGenerations[in.PoolID] = in.Metadata.Generation
	return nil
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
