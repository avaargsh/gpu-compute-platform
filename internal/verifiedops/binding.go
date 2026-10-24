package verifiedops

import (
    "errors"

    "github.com/avaargsh/gpu-compute-platform/internal/domain"
)

// Candidate identity is NOT an attestation; no API-server signature or
// owner-authorization conclusion is implied by this DTO.
type JobCreateUIDBinding struct {
    OperationID        string    `json:"operationId"`
    ClusterID          domain.ID `json:"clusterId"`
    ProjectID          string    `json:"projectId"`
    WorkloadID         domain.ID `json:"workloadId"`
    Generation         int64     `json:"generation"`
    Namespace          string    `json:"namespace"`
    JobName            string    `json:"jobName"`
    JobUID              string    `json:"jobUid"`
    JobResourceVersion string    `json:"jobResourceVersion"`
    LeaseOwner         string    `json:"leaseOwner"`
}

// JobCreateAttemptState is a durable reconciliation result, not a provider
// ownership verdict. UNKNOWN is deliberately non-retryable as a CREATE action.
type JobCreateAttemptState string

const (
    JobCreateAttemptNotFound JobCreateAttemptState = "NOT_FOUND"
    JobCreateAttemptUnknown  JobCreateAttemptState = "UNKNOWN"
    JobCreateAttemptACKBound JobCreateAttemptState = "ACK_BOUND"
    JobCreateAttemptStale    JobCreateAttemptState = "STALE"
)

// JobCreateAttemptResolution is returned only to the authenticated cluster
// Agent. Binding is an immutable candidate captured from the API-server CREATE
// response; consumers must still verify that a live Job has this UID.
type JobCreateAttemptResolution struct {
    State JobCreateAttemptState `json:"state"`
    Binding *JobCreateUIDBinding `json:"binding,omitempty"`
}

var ErrCreateUIDUnproven = errors.New("provider CREATE UID ownership unproven")
var ErrCreateUIDConflict = errors.New("conflicting provider CREATE UID binding")

// JobCreateAttempt is a durable, unverified intent to issue a CREATE.
// Persist it *before* contacting the API server; recording intent alone
// never proves that CREATE was sent, accepted, or returned a UID.
type JobCreateAttempt struct {
    OperationID string    `json:"operationId"`
    ClusterID   domain.ID `json:"clusterId"`
    ProjectID   string    `json:"projectId"`
    WorkloadID  domain.ID `json:"workloadId"`
    Generation  int64     `json:"generation"`
    Namespace   string    `json:"namespace"`
    JobName     string    `json:"jobName"`
    LeaseOwner  string    `json:"leaseOwner"`
}

var ErrCreateAttemptUnresolved = errors.New("provider CREATE attempt already prepared: outcome unresolved")
