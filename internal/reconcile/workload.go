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
	if clusterBinding.Provider == "" {
		return provider.WorkloadObservation{}, fmt.Errorf("cluster binding provider identity is required")
	}
	if !resourceReady(pool.Status, pool.Metadata.Generation) {
		return provider.WorkloadObservation{}, fmt.Errorf("compute pool not ready: %s", pool.Metadata.ID)
	}

	binding, err := resolveAcceleratorBinding(pool.Spec.AcceleratorBindings, workload.Spec.Accelerator.Class)
	if err != nil {
		return provider.WorkloadObservation{}, err
	}

	return r.provider.ReconcileWorkload(ctx, provider.WorkloadProjection{
		Provider:           clusterBinding.Provider,
		WorkloadID:         workload.Metadata.ID,
		ProjectID:          workload.ProjectID,
		PoolID:             workload.PoolID,
		ClusterID:          clusterBinding.ClusterID,
		Namespace:          projectBinding.Namespace,
		Generation:         workload.Metadata.Generation,
		Image:              workload.Spec.Image,
		Command:            workload.Spec.Command,
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
	return resolved, nil
}

func resourceReady(status domain.ResourceStatus, generation int64) bool {
	if status.ObservedGeneration != generation {
		return false
	}
	for _, condition := range status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			return true
		}
	}
	return false
}
