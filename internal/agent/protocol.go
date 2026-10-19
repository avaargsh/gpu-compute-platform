package agent

import (
	"context"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Registration struct {
	ClusterID         domain.ID                  `json:"clusterId"`
	AgentVersion      string                     `json:"agentVersion"`
	KubernetesVersion string                     `json:"kubernetesVersion"`
	Capabilities      domain.ClusterCapabilities `json:"capabilities"`
}

type AgentStatus struct {
	Registration
	RegisteredAt    time.Time  `json:"registeredAt"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
}

type Heartbeat struct {
	ClusterID domain.ID `json:"clusterId"`
	At        time.Time `json:"at"`
}

type DesiredResource struct {
	Kind       string    `json:"kind"`
	ID         domain.ID `json:"id"`
	Generation int64     `json:"generation"`
	// OperationID is an optional server-derived Management CREATE audit correlation.
	// It is never an authorization capability or proof of Kubernetes CREATE.
	OperationID       string         `json:"operationId,omitempty"`
	Spec              map[string]any `json:"spec"`
	DeletionTimestamp *time.Time     `json:"deletionTimestamp,omitempty"`
	Finalizers        []string       `json:"finalizers,omitempty"`
}

type ReconcileLeaseRequest struct {
	ClusterID  domain.ID `json:"clusterId"`
	Kind       string    `json:"kind"`
	ResourceID domain.ID `json:"resourceId"`
	Owner      string    `json:"owner"`
	TTLSeconds int64     `json:"ttlSeconds"`
}

type ReconcileLeaseResponse struct {
	Claimed bool `json:"claimed"`
}

type FinalizeDesiredRequest struct {
	ClusterID  domain.ID `json:"clusterId"`
	Kind       string    `json:"kind"`
	ResourceID domain.ID `json:"resourceId"`
	Generation int64     `json:"generation"`
	Owner      string    `json:"owner"`
}

type Observation struct {
	Kind               string             `json:"kind"`
	ID                 domain.ID          `json:"id"`
	ObservedGeneration int64              `json:"observedGeneration"`
	LeaseOwner         string             `json:"leaseOwner,omitempty"`
	Conditions         []domain.Condition `json:"conditions,omitempty"`
	EvidenceRefs       []string           `json:"evidenceRefs,omitempty"`
}

// JobCreateUIDCandidate travels only from an authenticated Cluster Agent to
// the server. The server must independently check immutable audit + lease.
// A returned 204 records an unverified candidate, not ownership proof.
type JobCreateUIDCandidate struct {
	ClusterID          domain.ID `json:"clusterId"`
	OperationID        string    `json:"operationId"`
	ProjectID          string    `json:"projectId"`
	WorkloadID         domain.ID `json:"workloadId"`
	Generation         int64     `json:"generation"`
	Namespace          string    `json:"namespace"`
	JobName            string    `json:"jobName"`
	JobUID             string    `json:"jobUid"`
	JobResourceVersion string    `json:"jobResourceVersion"`
	LeaseOwner         string    `json:"leaseOwner"`
}

// CreateUIDPublisher is separate from the legacy ControlPlane contract to
// preserve existing stores and local callers without any proof capability.
type CreateUIDPublisher interface {
	PublishJobCreateUID(context.Context, JobCreateUIDCandidate) error
}

type ControlPlane interface {
	Register(context.Context, Registration) error
	Heartbeat(context.Context, Heartbeat) error
	PullDesired(context.Context, domain.ID) ([]DesiredResource, error)
	Report(context.Context, domain.ID, []Observation) error
	ClaimReconcileLease(context.Context, ReconcileLeaseRequest) (bool, error)
	ReleaseReconcileLease(context.Context, ReconcileLeaseRequest) error
	FinalizeDesired(context.Context, FinalizeDesiredRequest) error
}
