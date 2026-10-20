package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	"k8s.io/apimachinery/pkg/types"
)

// IssuedObserverWork is internal Control Plane output. Its signed ticket is
// not an authorization verdict or permission to perform Kubernetes writes.
type IssuedObserverWork struct {
	Challenge verifiedops.ObserverSubject
	Expected  verifiedops.ExpectedJob
	Ticket    []byte
	ExpiresAt time.Time
}

// IssueSignedObserverWork atomically derives the expected Job from the
// audited CREATE/ACK and current Desired, then issues a fresh 256-bit
// challenge. Only after COMMIT does the caller-supplied dispatcher signer
// attest that exact DB-authorized context. A failed signer may leave an
// unused short-lived challenge; it can never produce a valid unsigned ticket.
//
// WARNING: internal trusted orchestration ONLY. Neither scope nor signer
// may be selected from an untrusted browser, Agent or Observer request.
func (s *Store) IssueSignedObserverWork(ctx context.Context, scope ObserverChallengeScope,
	signer verifiedops.ObserverSigner) (IssuedObserverWork, error) {
	var zero IssuedObserverWork
	if s == nil || s.db == nil {
		return zero, fmt.Errorf("postgres database is required")
	}
	if !bindingOperationID.MatchString(scope.OperationID) ||
		scope.ClusterID == "" || scope.ProjectID == "" ||
		scope.WorkloadID == "" || scope.Generation <= 0 || scope.JobUID == "" ||
		signer.KeyID == "" || signer.Signer == nil {
		return zero, verifiedops.ErrObserverUntrusted
	}
	pub, ok := signer.Signer.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return zero, verifiedops.ErrObserverUntrusted
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()

	var desiredGeneration int64
	var project, image string
	var deleting sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT generation,spec->>'projectID',
       COALESCE(NULLIF(spec->>'Image',''),spec->>'image',''),
       deletion_timestamp
FROM desired_resources
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
FOR SHARE
`, scope.ClusterID, scope.WorkloadID).Scan(&desiredGeneration, &project, &image, &deleting)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, ErrObserverNonceUnproven
	}
	if err != nil {
		return zero, err
	}
	if desiredGeneration != scope.Generation || project != scope.ProjectID || deleting.Valid {
		return zero, ErrObserverNonceUnproven
	}
	at := strings.LastIndex(image, "@sha256:")
	if at <= 0 || !verifiedops.ValidPinnedImageDigest(image[at+1:]) {
		return zero, ErrObserverNonceUnproven
	}

	// PREPARED and ACK must share the same lease owner and exact tuple.
	// Namespace comes from the project binding and original CREATE receipt,
	// never a subsequent GET, ticket request or Kubernetes label.
	var persistedProject, namespace, name, uid string
	err = tx.QueryRowContext(ctx, `
SELECT b.project_id,b.namespace,b.job_name,b.job_uid
FROM provider_job_create_uid_bindings b
JOIN provider_job_create_attempts p
  ON p.operation_id=b.operation_id AND p.cluster_id=b.cluster_id
  AND p.project_id=b.project_id AND p.workload_id=b.workload_id
  AND p.generation=b.generation AND p.namespace=b.namespace
  AND p.job_name=b.job_name AND p.lease_owner=b.lease_owner
JOIN management_operation_audit a
  ON a.request_id=b.operation_id AND a.cluster_id=b.cluster_id
  AND a.project_id=b.project_id AND a.kind='Workload'
  AND a.resource_id=b.workload_id AND a.action='create'
  AND a.resulting_generation=b.generation
JOIN project_bindings pb
  ON pb.project_id=b.project_id AND pb.cluster_id=b.cluster_id
  AND pb.namespace=b.namespace
WHERE b.operation_id=$1 AND b.cluster_id=$2
  AND b.workload_id=$3 AND b.generation=$4
FOR SHARE OF b
`, scope.OperationID, scope.ClusterID, scope.WorkloadID, scope.Generation).Scan(
		&persistedProject, &namespace, &name, &uid)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, ErrObserverNonceUnproven
	}
	if err != nil {
		return zero, err
	}
	if persistedProject != scope.ProjectID || uid != scope.JobUID {
		return zero, ErrObserverNonceUnproven
	}
	expected := verifiedops.ExpectedJob{
		Namespace: namespace, Name: name, JobUID: types.UID(uid),
		Generation: scope.Generation, ContainerName: expectedKueueContainer,
		ImageDigest: image[at+1:],
	}
	if !verifiedops.ValidExpectedJob(expected) {
		return zero, ErrObserverNonceUnproven
	}

	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return zero, fmt.Errorf("observer challenge CSPRNG unavailable: %w", err)
	}
	nonce := hex.EncodeToString(entropy)
	var issuedAt, expiresAt time.Time
	// One PostgreSQL timestamp is used for BOTH bounds: a ticket with
	// a hard two-minute maximum cannot fail because two clock_timestamp()
	// calls differed by a few microseconds.
	err = tx.QueryRowContext(ctx, `
WITH instant AS MATERIALIZED (SELECT clock_timestamp() AS t)
INSERT INTO verified_observer_challenges
 (cluster_id,nonce,operation_id,project_id,workload_id,generation,
  job_uid,issued_at,expires_at)
SELECT $1,$2,$3,$4,$5,$6,$7,instant.t,
       instant.t+($8 * interval '1 second')
FROM instant
RETURNING issued_at,expires_at
`, scope.ClusterID, nonce, scope.OperationID, scope.ProjectID,
		scope.WorkloadID, scope.Generation, scope.JobUID,
		int64(observerChallengeTTL/time.Second)).Scan(&issuedAt, &expiresAt)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}

	challenge := verifiedops.ObserverSubject{
		OperationID: scope.OperationID, ClusterID: string(scope.ClusterID),
		ProjectID: scope.ProjectID, WorkloadID: string(scope.WorkloadID),
		Generation: scope.Generation, JobUID: uid, Nonce: nonce,
	}
	work := verifiedops.ObserverWorkTicket{
		Challenge: challenge, Expected: expected,
		IssuedAt:  issuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano),
	}
	envelope, err := verifiedops.AuthorObserverWorkTicket(work, signer)
	if err != nil {
		return zero, err
	}
	return IssuedObserverWork{
		Challenge: challenge, Expected: expected,
		Ticket: envelope, ExpiresAt: expiresAt,
	}, nil
}
