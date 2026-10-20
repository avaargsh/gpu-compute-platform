package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

var ErrObserverNonceUnproven = errors.New("observer proof has no original CREATE identity")
var ErrObserverEvidenceReplayed = errors.New("observer nonce reused by a different proof")
var ErrObserverEvidenceStale = errors.New("observer evidence outside the freshness window")

var signedObserverNonce = regexp.MustCompile(`^[0-9a-f]{64}$`)

const observerEvidenceMaxAge = 5 * time.Minute
const observerEvidenceClockSkew = 30 * time.Second

// RecordSignedObserverNonce atomically fences signed proof replay against
// the original immutable Job CREATE candidate in PostgreSQL. It is an
// INTERNAL sink: trust must be loaded from a separately controlled root;
// expected context must not come from an unauthenticated caller.
//
// The first matching proof is recorded once; an identical retry is a
// harmless idempotent read, but a changed proof or cross-operation nonce
// is rejected. There is NO positive authorization/outcome verdict here.
func (s *Store) RecordSignedObserverNonce(ctx context.Context, envelope []byte,
	trust verifiedops.ObserverTrust, want verifiedops.ObserverSubject) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	if !bindingOperationID.MatchString(want.OperationID) ||
		!signedObserverNonce.MatchString(want.Nonce) || want.Generation <= 0 {
		return verifiedops.ErrObserverUntrusted
	}
	observed, err := time.Parse(time.RFC3339Nano, want.ObservedAt)
	if err != nil {
		return verifiedops.ErrObserverUntrusted
	}
	now := time.Now().UTC()
	if observed.Before(now.Add(-observerEvidenceMaxAge)) ||
		observed.After(now.Add(observerEvidenceClockSkew)) {
		return ErrObserverEvidenceStale
	}
	if _, err := verifiedops.VerifyObserverEnvelope(envelope, trust, want); err != nil {
		return err
	}
	sum := sha256.Sum256(envelope)
	digest := hex.EncodeToString(sum[:])

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Correlate the signed subject to the authenticated Agent's actual
	// CREATE response, never a Kubernetes GET or same-name adoption.
	// The audit row is immutable; UID binding exists only after PREPARED.
	var persistedProject, persistedUID string
	err = tx.QueryRowContext(ctx, `
SELECT b.project_id, b.job_uid
FROM provider_job_create_uid_bindings AS b
JOIN management_operation_audit AS a ON
  a.request_id=b.operation_id AND a.cluster_id=b.cluster_id
  AND a.project_id=b.project_id AND a.kind='Workload'
  AND a.resource_id=b.workload_id AND a.action='create'
  AND a.resulting_generation=b.generation
WHERE b.operation_id=$1 AND b.cluster_id=$2 AND b.workload_id=$3
  AND b.generation=$4
FOR SHARE OF b
`, want.OperationID, want.ClusterID, want.WorkloadID, want.Generation).Scan(
		&persistedProject, &persistedUID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrObserverNonceUnproven
	}
	if err != nil {
		return err
	}
	if persistedProject != want.ProjectID || persistedUID != want.JobUID {
		return ErrObserverNonceUnproven
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO verified_observer_nonce_receipts
 (cluster_id, nonce, operation_id, project_id, workload_id, generation,
  job_uid, signer_key_id, envelope_sha256, observed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (cluster_id, nonce) DO NOTHING
`, want.ClusterID, want.Nonce, want.OperationID, want.ProjectID,
		want.WorkloadID, want.Generation, want.JobUID, trust.KeyID, digest, observed)
	if err != nil {
		return err
	}

	var storedOperation, storedProject, storedWorkload, storedUID, storedSigner, storedDigest string
	var storedGeneration int64
	err = tx.QueryRowContext(ctx, `
SELECT operation_id, project_id, workload_id, generation,
       job_uid, signer_key_id, envelope_sha256
FROM verified_observer_nonce_receipts
WHERE cluster_id=$1 AND nonce=$2
`, want.ClusterID, want.Nonce).Scan(
		&storedOperation, &storedProject, &storedWorkload, &storedGeneration,
		&storedUID, &storedSigner, &storedDigest,
	)
	if err != nil {
		return err
	}
	if storedOperation != want.OperationID || storedProject != want.ProjectID ||
		storedWorkload != want.WorkloadID || storedGeneration != want.Generation ||
		storedUID != want.JobUID || storedSigner != trust.KeyID || storedDigest != digest {
		return ErrObserverEvidenceReplayed
	}
	return tx.Commit()
}
