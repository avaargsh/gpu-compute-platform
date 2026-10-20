package postgres

import (
	"context"
	"crypto/ed25519"
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

// RecordSignedObservedJobProof is an INTERNAL, non-HTTP v2 receipt admission.
// Its scope and trust root MUST originate from separate trusted orchestration.
// The expected namespace/name/UID/image are reconstructed from persisted
// Management Desired + project binding + original CREATE ACK, NOT the
// potentially attacker-authored signed assertion.
//
// The v1 and v2 predicates share the SAME (cluster, nonce) receipt table so
// a challenge cannot be consumed once under each protocol version. This
// records observed outcomes, including PENDING, but never issues a final
// AUTHORIZED_AND_VERIFIED verdict.
func (s *Store) RecordSignedObservedJobProof(
	ctx context.Context, envelope []byte, observerKeyID string,
	scope ObserverChallengeScope, nonce string,
) (ObservedJobReceipt, error) {
	var empty ObservedJobReceipt
	if s == nil || s.db == nil {
		return empty, fmt.Errorf("postgres database is required")
	}
	if !bindingOperationID.MatchString(scope.OperationID) || !signedObserverNonce.MatchString(nonce) ||
		scope.ClusterID == "" || scope.ProjectID == "" || scope.WorkloadID == "" ||
		scope.Generation <= 0 || strings.TrimSpace(scope.JobUID) == "" ||
		strings.TrimSpace(observerKeyID) == "" || len(observerKeyID) > 128 {
		return empty, verifiedops.ErrObserverUntrusted
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()

	// Resolve the trust root from the DBA-controlled, cluster-scoped registry.
	// FOR SHARE keeps revocation/rotation updates from racing this admission.
	var publicKey []byte
	err = tx.QueryRowContext(ctx, `
SELECT public_key
FROM verified_observer_signing_keys
WHERE cluster_id=$1 AND key_id=$2
  AND valid_from <= clock_timestamp()
  AND (valid_until IS NULL OR valid_until > clock_timestamp())
  AND revoked_at IS NULL
FOR SHARE
`, scope.ClusterID, observerKeyID).Scan(&publicKey)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, verifiedops.ErrObserverUntrusted
	}
	if err != nil {
		return empty, err
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return empty, verifiedops.ErrObserverUntrusted
	}
	trust := verifiedops.ObserverTrust{KeyID: observerKeyID, PublicKey: ed25519.PublicKey(publicKey)}

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
		scope.WorkloadID, scope.Generation, scope.JobUID, observerKeyID,
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
