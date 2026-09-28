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

type WorkloadProjection struct {
	WorkloadID  domain.ID
	ProjectID   domain.ID
	PoolID      domain.ID
	ClusterID   domain.ID
	Namespace   string
	Generation  int64
	Image       string
	Command     []string
	Accelerator domain.AcceleratorRequest
}

type WorkloadObservation struct {
	ObservedGeneration int64
	Phase              string
	Conditions         []domain.Condition
	EvidenceRefs       []string
}

type PoolProvider interface {
	ReconcilePool(context.Context, PoolProjection) (PoolObservation, error)
}

type WorkloadProvider interface {
	ReconcileWorkload(context.Context, WorkloadProjection) (WorkloadObservation, error)
}
