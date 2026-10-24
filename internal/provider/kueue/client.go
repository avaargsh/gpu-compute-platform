package kueue

import "context"

type JobObservation struct {
	// JobUID is the identity from the same fresh Job GET used for observation.
	// It is compared with the durable CREATE ACK candidate during recovery.
	JobUID        string
	Phase         string
	WorkloadName  string
	QuotaReserved bool
	Admitted      bool
	PodsReady     bool
	Succeeded     bool
	Failed        bool
	Message       string
}

// ClaimDeletionPreflight is a bounded, read-only assessment. Ready does not
// authorize deletion by itself: the subsequent DELETE must still re-check
// generation/owner and use Kubernetes UID/resourceVersion preconditions.
type ClaimDeletionPreflight struct {
	State  string
	Reason string
}

const (
	ClaimDeletionReady        = "Ready"
	ClaimDeletionAbsent       = "Absent"
	ClaimDeletionManualReview = "ManualReview"
)

type Client interface {
	ApplyResourceFlavor(context.Context, ResourceFlavor) error
	ApplyClusterQueue(context.Context, ClusterQueue) error
	ApplyLocalQueue(context.Context, LocalQueue) error
	ApplyResourceClaim(context.Context, Job) error
	ApplyJob(context.Context, Job) error
	ObserveJob(context.Context, string, string) (JobObservation, error)
	// InspectResourceClaim must not issue any Kubernetes write. It runs BEFORE
	// Job removal, preventing a legacy/unowned DRA claim from being orphaned.
	InspectResourceClaim(context.Context, string, string, string, int64) (ClaimDeletionPreflight, error)
	DeleteJob(context.Context, string, string, int64) (bool, error)
	DeleteResourceClaim(context.Context, string, string, string, int64) (bool, error)
	DeleteResourceFlavor(context.Context, string) (bool, error)
	DeleteClusterQueue(context.Context, string) (bool, error)
	DeleteLocalQueue(context.Context, string, string) (bool, error)
}
