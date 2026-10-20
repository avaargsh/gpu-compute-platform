package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	"k8s.io/apimachinery/pkg/types"
)

const expectedKueueContainer = "workload"

// ObservedJobReceipt is a signed *observation* receipt, not a proof of
// authorization, an authenticated Observer deployment, or a success verdict.
type ObservedJobReceipt struct {
	OperationID    string
	ClusterID      domain.ID
	Nonce          string
	PredicateType  string
	Observation    verifiedops.Observation
	ObservedAt     time.Time
	EnvelopeSHA256 string
}

// RecordSignedObservedJobProof admits one signed observation receipt. It retries
// bounded PostgreSQL serialization/deadlock failures because concurrent exact
// retries must be idempotent. Each attempt repeats all trusted-scope, challenge,
// signature and replay checks in a fresh serializable transaction.
func (s *Store) RecordSignedObservedJobProof(
	ctx context.Context, envelope []byte, trust verifiedops.ObserverTrust,
	scope ObserverChallengeScope, nonce string,
) (ObservedJobReceipt, error) {
	var empty ObservedJobReceipt
	const maxAttempts = 8
	for attempt := 0; attempt < maxAttempts; attempt++ {
		receipt, err := s.recordSignedObservedJobProofOnce(ctx, envelope, trust, scope, nonce)
		if err == nil || !isRetryableObserverTransactionError(err) || attempt == maxAttempts-1 {
			return receipt, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return empty, ctx.Err()
		case <-timer.C:
		}
	}
	return empty, fmt.Errorf("observer receipt transaction retry exhausted")
}

func isRetryableObserverTransactionError(err error) bool {
	var state interface{ SQLState() string }
	if !errors.As(err, &state) {
		return false
	}
	switch state.SQLState() {
	case "40001", "40P01": // serialization_failure, deadlock_detected
		return true
	default:
		return false
	}
}

// recordSignedObservedJobProofOnce performs one serializable receipt-admission
// attempt. Scope and trust must originate from separate trusted orchestration.
// Expected namespace/name/UID/image are reconstructed from persisted
// Management Desired + project binding + original CREATE ACK, not from the
// potentially attacker-authored signed assertion.
//
// The v1 and v2 predicates share the same (cluster, nonce) receipt table so
// a challenge cannot be consumed once under each protocol version. This
// records observed outcomes, including PENDING, but never issues a final
// AUTHORIZED_AND_VERIFIED verdict.
func (s *Store) recordSignedObservedJobProofOnce(
	ctx context.Context, envelope []byte, trust verifiedops.ObserverTrust,
	scope ObserverChallengeScope, nonce string,
) (ObservedJobReceipt, error) {
	var empty ObservedJobReceipt
	if s == nil || s.db == nil {
		return empty, fmt.Errorf("postgres database is required")
	}
	if !bindingOperationID.MatchString(scope.OperationID) || !signedObserverNonce.MatchString(nonce) ||
		scope.ClusterID == "" || scope.ProjectID == "" || scope.WorkloadID == "" ||
		scope.Generation <= 0 || strings.TrimSpace(scope.JobUID) == "" {
		return empty, verifiedops.ErrObserverUntrusted
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()

	// Canonical order matches the previous nonce sink: Desired -> original
	// CREATE UID -> server Challenge -> nonce receipt.
	var desiredGeneration int64
	var desiredProject, rawImage string
	var deleting sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT generation, spec->>'projectID',
       COALESCE(NULLIF(spec->>'Image',''),spec->>'image',''),
       deletion_timestamp
FROM desired_resources
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
FOR SHARE
`, scope.ClusterID, scope.WorkloadID).Scan(
		&desiredGeneration, &desiredProject, &rawImage, &deleting)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrObserverNonceUnproven
	}
	if err != nil {
		return empty, err
	}
	if desiredGeneration != scope.Generation || desiredProject != scope.ProjectID || deleting.Valid {
		return empty, ErrObserverNonceUnproven
	}
	imageAt := strings.LastIndex(rawImage, "@sha256:")
	if imageAt <= 0 || imageAt+1 >= len(rawImage) {
		return empty, ErrObserverNonceUnproven
	}
	digest := rawImage[imageAt+1:]
	if !verifiedops.ValidPinnedImageDigest(digest) {
		return empty, ErrObserverNonceUnproven
	}

	// Only actual CREATE response identity and project binding can determine
	// the expected Job. A GET by name alone never populates this table.
	var project, namespace, name, uid string
	err = tx.QueryRowContext(ctx, `
SELECT b.project_id,b.namespace,b.job_name,b.job_uid
FROM provider_job_create_uid_bindings AS b
JOIN provider_job_create_attempts AS p
  ON p.operation_id=b.operation_id AND p.cluster_id=b.cluster_id
  AND p.project_id=b.project_id AND p.workload_id=b.workload_id
  AND p.generation=b.generation AND p.lease_owner=b.lease_owner
  AND p.namespace=b.namespace AND p.job_name=b.job_name
JOIN management_operation_audit AS a
  ON a.request_id=b.operation_id AND a.cluster_id=b.cluster_id
  AND a.project_id=b.project_id AND a.kind='Workload'
  AND a.resource_id=b.workload_id AND a.action='create'
  AND a.resulting_generation=b.generation
JOIN project_bindings AS pb
  ON pb.project_id=b.project_id AND pb.cluster_id=b.cluster_id
  AND pb.namespace=b.namespace
WHERE b.operation_id=$1 AND b.cluster_id=$2 AND b.workload_id=$3
  AND b.generation=$4
FOR SHARE OF b
`, scope.OperationID, scope.ClusterID, scope.WorkloadID, scope.Generation).Scan(
		&project, &namespace, &name, &uid)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrObserverNonceUnproven
	}
	if err != nil {
		return empty, err
	}
	if project != scope.ProjectID || uid != scope.JobUID {
		return empty, ErrObserverNonceUnproven
	}
	expected := verifiedops.ExpectedJob{
		Namespace: namespace, Name: name, JobUID: types.UID(uid),
		Generation: scope.Generation, ContainerName: expectedKueueContainer,
		ImageDigest: digest,
	}
	if !verifiedops.ValidExpectedJob(expected) {
		return empty, ErrObserverNonceUnproven
	}

	var challengeOperation, challengeProject, challengeWorkload, challengeUID string
	var challengeGeneration int64
	var issuedAt, expiresAt time.Time
	err = tx.QueryRowContext(ctx, `
SELECT operation_id,project_id,workload_id,generation,job_uid,issued_at,expires_at
FROM verified_observer_challenges
WHERE cluster_id=$1 AND nonce=$2 FOR UPDATE
`, scope.ClusterID, nonce).Scan(
		&challengeOperation, &challengeProject, &challengeWorkload,
		&challengeGeneration, &challengeUID, &issuedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrObserverChallengeMissing
	}
	if err != nil {
		return empty, err
	}
	if challengeOperation != scope.OperationID || challengeProject != scope.ProjectID ||
		challengeWorkload != string(scope.WorkloadID) ||
		challengeGeneration != scope.Generation || challengeUID != scope.JobUID {
		return empty, ErrObserverChallengeMismatch
	}

	// Always authenticate the exact v2 signed body before consuming anything.
	want := verifiedops.ObserverSubject{
		OperationID: scope.OperationID, ClusterID: string(scope.ClusterID),
		ProjectID: scope.ProjectID, WorkloadID: string(scope.WorkloadID),
		Generation: scope.Generation, JobUID: scope.JobUID, Nonce: nonce,
	}
	proof, err := verifiedops.VerifyObservedJobEnvelope(envelope, trust, want, expected)
	if err != nil {
		return empty, err
	}
	observed, err := time.Parse(time.RFC3339Nano, proof.Subject.ObservedAt)
	if err != nil {
		return empty, verifiedops.ErrObserverUntrusted
	}
	var alive bool
	err = tx.QueryRowContext(ctx,
		`SELECT clock_timestamp() < $1::timestamptz`, expiresAt).Scan(&alive)
	if err != nil {
		return empty, err
	}
	if !alive {
		return empty, ErrObserverChallengeExpired
	}
	if observed.Before(issuedAt.Add(-observerEvidenceClockSkew)) {
		return empty, ErrObserverChallengeMismatch
	}
	obsBytes, err := json.Marshal(proof.Observation)
	if err != nil {
		return empty, err
	}
	dig := sha256.Sum256(envelope)
	envelopeDigest := hex.EncodeToString(dig[:])
	_, err = tx.ExecContext(ctx, `
INSERT INTO verified_observer_nonce_receipts
 (cluster_id,nonce,operation_id,project_id,workload_id,generation,
  job_uid,signer_key_id,predicate_type,observation_json,envelope_sha256,observed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)
ON CONFLICT (cluster_id,nonce) DO NOTHING
`, scope.ClusterID, nonce, scope.OperationID, scope.ProjectID,
		scope.WorkloadID, scope.Generation, scope.JobUID, trust.KeyID,
		verifiedops.ObservedJobPredicateType, string(obsBytes), envelopeDigest, observed)
	if err != nil {
		return empty, err
	}
	var storedOperation, storedProject, storedWorkload, storedUID string
	var storedSigner, storedType, storedDigest string
	var storedGeneration int64
	err = tx.QueryRowContext(ctx, `
SELECT operation_id,project_id,workload_id,generation,job_uid,
       signer_key_id,predicate_type,envelope_sha256
FROM verified_observer_nonce_receipts
WHERE cluster_id=$1 AND nonce=$2
`, scope.ClusterID, nonce).Scan(
		&storedOperation, &storedProject, &storedWorkload, &storedGeneration,
		&storedUID, &storedSigner, &storedType, &storedDigest)
	if err != nil {
		return empty, err
	}
	if storedOperation != scope.OperationID || storedProject != scope.ProjectID ||
		storedWorkload != string(scope.WorkloadID) ||
		storedGeneration != scope.Generation || storedUID != scope.JobUID ||
		storedSigner != trust.KeyID || storedType != verifiedops.ObservedJobPredicateType ||
		storedDigest != envelopeDigest {
		return empty, ErrObserverEvidenceReplayed
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return ObservedJobReceipt{
		OperationID: scope.OperationID, ClusterID: scope.ClusterID, Nonce: nonce,
		PredicateType: verifiedops.ObservedJobPredicateType,
		Observation:   proof.Observation, ObservedAt: observed,
		EnvelopeSHA256: envelopeDigest,
	}, nil
}
