package verifiedops

import (
	"errors"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

// Candidate identity is NOT an attestation; no API-server signature or
// owner-authorization conclusion is implied by this DTO.
type JobCreateUIDBinding struct {
	OperationID        string
	ClusterID          domain.ID
	ProjectID          string
	WorkloadID         domain.ID
	Generation         int64
	Namespace          string
	JobName            string
	JobUID             string
	JobResourceVersion string
	LeaseOwner         string
}

var ErrCreateUIDUnproven = errors.New("provider CREATE UID ownership unproven")
var ErrCreateUIDConflict = errors.New("conflicting provider CREATE UID binding")

// JobCreateAttempt is a durable, unverified intent to issue a CREATE.
// Persist it *before* contacting the API server; recording intent alone
// never proves that CREATE was sent, accepted, or returned a UID.
type JobCreateAttempt struct {
	OperationID string `json:"operationId"`
	ClusterID domain.ID `json:"clusterId"`
	ProjectID string `json:"projectId"`
	WorkloadID domain.ID `json:"workloadId"`
	Generation int64 `json:"generation"`
	Namespace string `json:"namespace"`
	JobName string `json:"jobName"`
	LeaseOwner string `json:"leaseOwner"`
}

var ErrCreateAttemptUnresolved = errors.New("provider CREATE attempt already prepared: outcome unresolved")
