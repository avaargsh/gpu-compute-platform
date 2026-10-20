package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

var ErrObserverChallengeMissing = errors.New("observer challenge not issued")
var ErrObserverChallengeExpired = errors.New("observer challenge expired")
var ErrObserverChallengeMismatch = errors.New("observer challenge context mismatch")

const observerChallengeTTL = 2 * time.Minute

// ObserverChallengeScope is supplied by trusted Control Plane orchestration,
// NOT a browser, external Agent, or unsigned Observer request. The database
// checks this scope against the original audited CREATE response.
type ObserverChallengeScope struct {
	OperationID string
	ClusterID   domain.ID
	ProjectID   string
	WorkloadID  domain.ID
	Generation  int64
	JobUID      string
}

type ObserverChallenge struct {
	ObserverChallengeScope
	Nonce     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// IssueObserverChallenge produces a cryptographically unpredictable 256-bit
// server nonce. The original immutable Management audit and API-server CREATE
// ACK candidate MUST exist for the exact requested operation/UID before an
// issuer can store a challenge. No HTTP endpoint grants this authority.
//
// This method alone does not prove the trusted signer's authorization or
// the Kubernetes runtime result. Do not expose it via public Agent routing.
func (s *Store) IssueObserverChallenge(ctx context.Context, scope ObserverChallengeScope) (ObserverChallenge, error) {
	if s == nil || s.db == nil {
		return ObserverChallenge{}, fmt.Errorf("postgres database is required")
	}
	if !bindingOperationID.MatchString(scope.OperationID) ||
		scope.ClusterID == "" || scope.ProjectID == "" || scope.WorkloadID == "" ||
		scope.Generation < 1 || scope.JobUID == "" {
		return ObserverChallenge{}, verifiedops.ErrObserverUntrusted
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ObserverChallenge{}, err
	}
	defer tx.Rollback()

	// Never issue or consume an operation challenge after the Desired has
	// advanced to another generation or entered deletion. Canonical lock
	// order is Desired first, then provider identity and challenge rows.
	var generation int64
	var desiredProject string
	var deleting sql.NullTime
	err := tx.QueryRowContext(ctx, `
SELECT generation,spec->>'projectID',deletion_timestamp
FROM desired_resources
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
FOR SHARE
`, scope.ClusterID,scope.WorkloadID).Scan(&generation,&desiredProject,&deleting)
	if errors.Is(err,sql.ErrNoRows) {
		return ObserverChallenge{}, ErrObserverNonceUnproven
	}
	if err != nil {
		return ObserverChallenge{},err
	}
	if generation!=scope.Generation || desiredProject!=scope.ProjectID || deleting.Valid {
		return ObserverChallenge{}, ErrObserverNonceUnproven
	}

	// The immutable candidate was written only after a durable PREPARED
	// and a real CREATE response while the owning Agent held its lease.
	var project, uid string
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
`, scope.OperationID, scope.ClusterID, scope.WorkloadID, scope.Generation).Scan(&project, &uid)
	if errors.Is(err, sql.ErrNoRows) {
		return ObserverChallenge{}, ErrObserverNonceUnproven
	}
	if err != nil {
		return ObserverChallenge{}, err
	}
	if project != scope.ProjectID || uid != scope.JobUID {
		return ObserverChallenge{}, ErrObserverNonceUnproven
	}

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return ObserverChallenge{}, fmt.Errorf("secure observer nonce generation unavailable: %w", err)
	}
	challenge := ObserverChallenge{ObserverChallengeScope: scope, Nonce: hex.EncodeToString(nonceBytes)}
	err = tx.QueryRowContext(ctx, `
INSERT INTO verified_observer_challenges
 (cluster_id,nonce,operation_id,project_id,workload_id,generation,job_uid,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,clock_timestamp()+($8 * interval '1 second'))
RETURNING issued_at, expires_at
`, scope.ClusterID, challenge.Nonce, scope.OperationID, scope.ProjectID,
		scope.WorkloadID, scope.Generation, scope.JobUID,
		int64(observerChallengeTTL/time.Second)).Scan(&challenge.IssuedAt, &challenge.ExpiresAt)
	if err != nil {
		return ObserverChallenge{}, err
	}
	if err := tx.Commit(); err != nil {
		return ObserverChallenge{}, err
	}
	return challenge, nil
}
