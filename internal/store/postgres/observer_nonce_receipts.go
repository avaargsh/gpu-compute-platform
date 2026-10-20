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
// Only an unexpired, server-issued challenge permits a first receipt.
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

	// Recheck current Desired before binding evidence; a challenge that was
	// minted before delete-request/generation change cannot certify it.
	var desiredGeneration int64
	var desiredProject string
	var deleting sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT generation,spec->>'projectID',deletion_timestamp
FROM desired_resources
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
FOR SHARE
`, want.ClusterID,want.WorkloadID).Scan(&desiredGeneration,&desiredProject,&deleting)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrObserverNonceUnproven
	}
	if err != nil {
		return err
	}
	if desiredGeneration!=want.Generation || desiredProject!=want.ProjectID || deleting.Valid {
		return ErrObserverNonceUnproven
	}

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

	// The challenge row MUST be locked and checked in the same transaction
	// as inserting the signed-proof receipt. Neither a guessed nonce nor
	// one issued for another operation can produce a valid receipt.
	var challengeOperation, challengeProject, challengeWorkload, challengeUID string
	var challengeGeneration int64
	var issuedAt, expiresAt time.Time
	err = tx.QueryRowContext(ctx, `
SELECT operation_id,project_id,workload_id,generation,job_uid,issued_at,expires_at
FROM verified_observer_challenges
WHERE cluster_id=$1 AND nonce=$2
FOR UPDATE
`, want.ClusterID, want.Nonce).Scan(
		&challengeOperation, &challengeProject, &challengeWorkload, &challengeGeneration,
		&challengeUID, &issuedAt, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrObserverChallengeMissing
	}
	if err != nil {
		return err
	}
	if challengeOperation != want.OperationID || challengeProject != want.ProjectID ||
		challengeWorkload != want.WorkloadID || challengeGeneration != want.Generation ||
		challengeUID != want.JobUID {
		return ErrObserverChallengeMismatch
	}
	// Use PostgreSQL clock_timestamp for expiry; a stale request must not
	// revive a challenge by supplying a forged signed wall-clock timestamp.
	var challengeAlive bool
	if err := tx.QueryRowContext(ctx,
		`SELECT clock_timestamp() < $1::timestamptz`, expiresAt).Scan(&challengeAlive); err != nil {
		return err
	}
	if !challengeAlive {
		return ErrObserverChallengeExpired
	}
	// Do not accept proof claiming observation before challenge issuance.
	// A bounded 30s lag permits real Observer/DB clock skew, not a retry
	// from before this independent challenge was created.
	if observed.Before(issuedAt.Add(-observerEvidenceClockSkew)) {
		return ErrObserverChallengeMismatch
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
