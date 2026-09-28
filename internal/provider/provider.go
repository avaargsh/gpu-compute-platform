package provider

import (
	"context"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type PoolProjection struct {
	PoolID       domain.ID
	ProjectID    domain.ID
	ClusterID    domain.ID
	Namespace    string
	Generation   int64
	Accelerators []domain.AcceleratorRequest
	Scheduling   domain.SchedulingPolicy
}

type PoolObservation struct {
	ObservedGeneration int64
	Conditions         []domain.Condition
	EvidenceRefs       []string
}

type ComputeProvider interface {
	ReconcilePool(context.Context, PoolProjection) (PoolObservation, error)
}
