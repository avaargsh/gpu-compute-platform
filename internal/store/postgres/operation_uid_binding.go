package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Re-export neutral contract symbols so existing Store callers and tests
// retain their source API without importing the HTTP layer.
var ErrCreateUIDUnproven = verifiedops.ErrCreateUIDUnproven
var ErrCreateUIDConflict = verifiedops.ErrCreateUIDConflict

var bindingOperationID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var bindingDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Only a trusted Agent carrying a genuine Kubernetes CREATE response may
// construct this value; same-generation GET/adoption is never enough.
type JobCreateUIDBinding = verifiedops.JobCreateUIDBinding

func validJobBinding(in JobCreateUIDBinding) bool {
	return bindingOperationID.MatchString(in.OperationID) &&
		in.ClusterID != "" && in.ProjectID != "" && in.WorkloadID != "" &&
		in.Generation > 0 && strings.TrimSpace(in.LeaseOwner) != "" &&
		strings.TrimSpace(in.JobUID) != "" && strings.TrimSpace(in.JobResourceVersion) != "" &&
		len(validation.IsDNS1123Label(in.Namespace)) == 0 &&
		len(validation.IsDNS1123Subdomain(in.JobName)) == 0 &&
		in.JobName == "job-"+strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(string(in.WorkloadID)))
}

func equalJobBinding(a, b JobCreateUIDBinding) bool {
	return a.OperationID == b.OperationID && a.ClusterID == b.ClusterID &&
		a.ProjectID == b.ProjectID && a.WorkloadID == b.WorkloadID &&
		a.Generation == b.Generation && a.Namespace == b.Namespace &&
		a.JobName == b.JobName && a.JobUID == b.JobUID &&
		a.JobResourceVersion == b.JobResourceVersion && a.LeaseOwner == b.LeaseOwner
}

// RecordJobCreateUID binds a candidate UID to an immutable Management CREATE
// audit only while Desired, project/pool binding and Agent lease agree. It
// cannot authenticate the Kubernetes response itself; that requires a trusted
// Agent ingress and independent observer. No user-facing API invokes this.
func (s *Store) RecordJobCreateUID(ctx context.Context, in JobCreateUIDBinding) error {
	if !validJobBinding(in) {
		return ErrCreateUIDUnproven
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock order must remain Desired -> Lease, as in Report/Finalize.
	var generation int64
	var desiredProject, pool string
	var deleting sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT generation, spec->>'projectID', spec->>'poolID', deletion_timestamp
FROM desired_resources
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
FOR SHARE
`, in.ClusterID, in.WorkloadID).Scan(&generation, &desiredProject, &pool, &deleting)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCreateUIDUnproven
	}
	if err != nil {
		return err
	}
	if generation != in.Generation || desiredProject != in.ProjectID || pool == "" || deleting.Valid {
		return ErrCreateUIDUnproven
	}
	var owner string
	var leaseValid bool
	err = tx.QueryRowContext(ctx, `
SELECT owner, lease_until > clock_timestamp()
FROM reconcile_leases
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
FOR UPDATE
`, in.ClusterID, in.WorkloadID).Scan(&owner, &leaseValid)
	if errors.Is(err, sql.ErrNoRows) {
		return agentstore.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if owner != in.LeaseOwner || !leaseValid {
		return agentstore.ErrLeaseLost
	}

	// The original Operation ID must exist in the immutable audit; absent
	// historical fingerprints cannot be upgraded into proof retroactively.
	var digest string
	err = tx.QueryRowContext(ctx, `
SELECT request_digest FROM management_operation_audit
WHERE request_id=$1 AND cluster_id=$2 AND project_id=$3
  AND kind='Workload' AND resource_id=$4 AND action='create'
  AND resulting_generation=$5
`, in.OperationID, in.ClusterID, in.ProjectID, in.WorkloadID, in.Generation).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCreateUIDUnproven
	}
	if err != nil {
		return err
	}
	if !bindingDigest.MatchString(digest) || digest == strings.Repeat("0", 64) {
		return ErrCreateUIDUnproven
	}

	var namespace, provider string
	err = tx.QueryRowContext(ctx, `
SELECT namespace FROM project_bindings WHERE project_id=$1 AND cluster_id=$2
`, in.ProjectID, in.ClusterID).Scan(&namespace)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCreateUIDUnproven
	}
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `
SELECT provider FROM cluster_bindings WHERE pool_id=$1 AND cluster_id=$2
`, pool, in.ClusterID).Scan(&provider)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCreateUIDUnproven
	}
	if err != nil {
		return err
	}
	if namespace != in.Namespace || provider != "kueue" {
		return ErrCreateUIDUnproven
	}

	// First successful binding wins; neither a replacement UID nor another
	// operation for this same Workload generation may overwrite it.
	_, err = tx.ExecContext(ctx, `
INSERT INTO provider_job_create_uid_bindings
 (operation_id, cluster_id, project_id, workload_id, generation,
  namespace, job_name, job_uid, job_resource_version, lease_owner)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT DO NOTHING
`, in.OperationID, in.ClusterID, in.ProjectID, in.WorkloadID, in.Generation,
		in.Namespace, in.JobName, in.JobUID, in.JobResourceVersion, in.LeaseOwner)
	if err != nil {
		return err
	}
	var stored JobCreateUIDBinding
	err = tx.QueryRowContext(ctx, `
SELECT operation_id, cluster_id, project_id, workload_id, generation,
       namespace, job_name, job_uid, job_resource_version, lease_owner
FROM provider_job_create_uid_bindings WHERE operation_id=$1
`, in.OperationID).Scan(
		&stored.OperationID, &stored.ClusterID, &stored.ProjectID, &stored.WorkloadID,
		&stored.Generation, &stored.Namespace, &stored.JobName, &stored.JobUID,
		&stored.JobResourceVersion, &stored.LeaseOwner,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCreateUIDConflict
	}
	if err != nil {
		return err
	}
	if !equalJobBinding(in, stored) {
		return ErrCreateUIDConflict
	}
	return tx.Commit()
}

// Trusted internal read only. Never return it directly as a user grant,
// SIGNED evidence, or an operation-ownership determination.
func (s *Store) GetJobCreateUID(ctx context.Context, operationID string,
	clusterID domain.ID, workloadID domain.ID, generation int64,
) (JobCreateUIDBinding, bool, error) {
	if s == nil || s.db == nil {
		return JobCreateUIDBinding{}, false, fmt.Errorf("postgres database is required")
	}
	if !bindingOperationID.MatchString(operationID) || clusterID == "" ||
		workloadID == "" || generation < 1 {
		return JobCreateUIDBinding{}, false, ErrCreateUIDUnproven
	}
	var out JobCreateUIDBinding
	err := s.db.QueryRowContext(ctx, `
SELECT operation_id, cluster_id, project_id, workload_id, generation,
       namespace, job_name, job_uid, job_resource_version, lease_owner
FROM provider_job_create_uid_bindings
WHERE operation_id=$1 AND cluster_id=$2 AND workload_id=$3 AND generation=$4
`, operationID, clusterID, workloadID, generation).Scan(
		&out.OperationID, &out.ClusterID, &out.ProjectID, &out.WorkloadID,
		&out.Generation, &out.Namespace, &out.JobName, &out.JobUID,
		&out.JobResourceVersion, &out.LeaseOwner,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return JobCreateUIDBinding{}, false, nil
	}
	return out, err == nil, err
}
