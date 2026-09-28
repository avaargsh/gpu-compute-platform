package reconcile

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type WorkloadReconciler struct {
	provider provider.WorkloadProvider
}

func NewWorkloadReconciler(p provider.WorkloadProvider) *WorkloadReconciler {
	return &WorkloadReconciler{provider: p}
}

func (r *WorkloadReconciler) Reconcile(
	ctx context.Context,
	workload domain.Workload,
	pool domain.ComputePool,
	projectBinding domain.ProjectBinding,
	clusterBinding domain.ClusterBinding,
) (provider.WorkloadObservation, error) {
	if r.provider == nil {
		return provider.WorkloadObservation{}, fmt.Errorf("compute provider is required")
	}
	if workload.ProjectID != pool.ProjectID || workload.PoolID != pool.Metadata.ID {
		return provider.WorkloadObservation{}, fmt.Errorf("workload does not belong to compute pool")
	}
	if projectBinding.ProjectID != workload.ProjectID {
		return provider.WorkloadObservation{}, fmt.Errorf("project binding does not belong to workload project")
	}
	if clusterBinding.PoolID != pool.Metadata.ID || clusterBinding.ClusterID != projectBinding.ClusterID {
		return provider.WorkloadObservation{}, fmt.Errorf("invalid cluster placement")
	}

	binding, err := resolveAcceleratorBinding(pool.Spec.AcceleratorBindings, workload.Spec.Accelerator.Class)
	if err != nil {
		return provider.WorkloadObservation{}, err
	}

	return r.provider.ReconcileWorkload(ctx, provider.WorkloadProjection{
		WorkloadID:  workload.Metadata.ID,
		ProjectID:   workload.ProjectID,
		PoolID:      workload.PoolID,
		ClusterID:   clusterBinding.ClusterID,
		Namespace:   projectBinding.Namespace,
		Generation:  workload.Metadata.Generation,
		Image:       workload.Spec.Image,
		Command:     workload.Spec.Command,
		Accelerator:        workload.Spec.Accelerator,
		AcceleratorBinding: binding,
	})
}

func resolveAcceleratorBinding(bindings []domain.AcceleratorBinding, class string) (domain.AcceleratorBinding, error) {
	if class == "" {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator class is required")
	}
	var resolved domain.AcceleratorBinding
	for _, binding := range bindings {
		if binding.Class != class {
			continue
		}
		if resolved.Class != "" {
			return domain.AcceleratorBinding{}, fmt.Errorf("duplicate accelerator binding: %s", class)
		}
		resolved = binding
	}
	if resolved.Class == "" {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator binding not found: %s", class)
	}
	if resolved.ResourceName == "" || resolved.Flavor == "" {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator binding is incomplete: %s", class)
	}
	return resolved, nil
}
