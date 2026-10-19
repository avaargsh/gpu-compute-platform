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
