package reconcile

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type PoolReconciler struct {
	provider provider.PoolProvider
}

func NewPoolReconciler(p provider.PoolProvider) *PoolReconciler {
	return &PoolReconciler{provider: p}
}

func (r *PoolReconciler) Reconcile(
	ctx context.Context,
	pool domain.ComputePool,
	projectBinding domain.ProjectBinding,
	clusterBinding domain.ClusterBinding,
) (domain.ResourceStatus, []string, error) {
	if r.provider == nil {
		return domain.ResourceStatus{}, nil, fmt.Errorf("compute provider is required")
	}
	if projectBinding.ProjectID != pool.ProjectID {
		return domain.ResourceStatus{}, nil, fmt.Errorf("project binding does not belong to pool project")
	}
	if clusterBinding.PoolID != pool.Metadata.ID {
		return domain.ResourceStatus{}, nil, fmt.Errorf("cluster binding does not belong to pool")
	}
	if projectBinding.ClusterID != clusterBinding.ClusterID {
		return domain.ResourceStatus{}, nil, fmt.Errorf("project and pool must target the same cluster")
	}
	if clusterBinding.Provider == "" {
		return domain.ResourceStatus{}, nil, fmt.Errorf("cluster binding provider identity is required")
	}

	observed, err := r.provider.ReconcilePool(ctx, provider.PoolProjection{
		Provider:            clusterBinding.Provider,
		PoolID:              pool.Metadata.ID,
		ProjectID:           pool.ProjectID,
		ClusterID:           clusterBinding.ClusterID,
		Namespace:           projectBinding.Namespace,
		Generation:          pool.Metadata.Generation,
		Accelerators:        pool.Spec.Accelerators,
		AcceleratorBindings: pool.Spec.AcceleratorBindings,
		Scheduling:          pool.Spec.Scheduling,
	})
	if err != nil {
		return domain.ResourceStatus{}, nil, err
	}

	return domain.ResourceStatus{
		ObservedGeneration: observed.ObservedGeneration,
		Conditions:         observed.Conditions,
	}, observed.EvidenceRefs, nil
}
