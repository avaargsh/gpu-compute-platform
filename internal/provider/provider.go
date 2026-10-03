package provider

import (
	"context"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type PoolProjection struct {
	Provider            string
	PoolID              domain.ID
	ProjectID           domain.ID
	ClusterID           domain.ID
	Namespace           string
	Generation          int64
	Accelerators        []domain.AcceleratorRequest
	AcceleratorBindings []domain.AcceleratorBinding
	Scheduling          domain.SchedulingPolicy
}

type PoolObservation struct {
	ObservedGeneration int64
	Conditions         []domain.Condition
	EvidenceRefs       []string
}

type WorkloadProjection struct {
	Provider           string
	WorkloadID         domain.ID
	ProjectID          domain.ID
	PoolID             domain.ID
	ClusterID          domain.ID
	Namespace          string
	Generation         int64
	Image              string
	Command            []string
	Accelerator        domain.AcceleratorRequest
	AcceleratorBinding domain.AcceleratorBinding
}

type WorkloadObservation struct {
	ObservedGeneration int64
	Phase              string
	Conditions         []domain.Condition
	EvidenceRefs       []string
}

type DeletionObservation struct {
	Gone         bool
	EvidenceRefs []string
}

type PoolProvider interface {
	ReconcilePool(context.Context, PoolProjection) (PoolObservation, error)
	DeletePool(context.Context, PoolProjection) (DeletionObservation, error)
}

type WorkloadProvider interface {
	ReconcileWorkload(context.Context, WorkloadProjection) (WorkloadObservation, error)
	DeleteWorkload(context.Context, WorkloadProjection) (DeletionObservation, error)
}

type Adapter interface {
	PoolProvider
	WorkloadProvider
}
