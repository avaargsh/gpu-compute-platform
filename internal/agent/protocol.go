package agent

import (
	"context"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Registration struct {
	ClusterID         domain.ID `json:"clusterId"`
	AgentVersion      string    `json:"agentVersion"`
	KubernetesVersion string    `json:"kubernetesVersion"`
}

type Heartbeat struct {
	ClusterID domain.ID `json:"clusterId"`
	At        time.Time `json:"at"`
}

type DesiredResource struct {
	Kind              string         `json:"kind"`
	ID                domain.ID      `json:"id"`
	Generation        int64          `json:"generation"`
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
}

type Observation struct {
	Kind               string             `json:"kind"`
	ID                 domain.ID          `json:"id"`
	ObservedGeneration int64              `json:"observedGeneration"`
	Conditions         []domain.Condition `json:"conditions,omitempty"`
	EvidenceRefs       []string           `json:"evidenceRefs,omitempty"`
}

type ControlPlane interface {
	Register(context.Context, Registration) error
	Heartbeat(context.Context, Heartbeat) error
	PullDesired(context.Context, domain.ID) ([]DesiredResource, error)
	Report(context.Context, domain.ID, []Observation) error
	ClaimReconcileLease(context.Context, ReconcileLeaseRequest) (ReconcileLeaseGrant, error)
	ReleaseReconcileLease(context.Context, ReconcileLeaseRequest) error
	FinalizeDesired(context.Context, FinalizeDesiredRequest) error
}
