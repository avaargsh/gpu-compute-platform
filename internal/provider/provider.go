package provider

import (
	"context"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Adapter interface {
	PoolProvider
	WorkloadProvider
}

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
	// OperationID is a non-authorizing original Management audit correlation.
	OperationID        string
	// OnCreateACK is invoked only for a genuine Job CREATE return. This
	// ephemeral callback is never serialized, replayed, or inferred from GET.
	OnCreateACK       func(context.Context, JobCreateACK) error `json:"-"`
	Image              string
	Command            []string
	Accelerator        domain.AcceleratorRequest
	AcceleratorBinding domain.AcceleratorBinding
	Workspace          *domain.WorkspaceMount
}

// JobCreateACK records only the API-server-issued UID/RV from a successful
// CREATE response; it is a candidate identity, not signed execution proof.
type JobCreateACK struct {
	Namespace       string
	Name            string
	UID             string
	ResourceVersion string
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
