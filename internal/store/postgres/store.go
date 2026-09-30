package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Register(ctx context.Context, in agent.Registration) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cluster_agents (cluster_id, agent_version, kubernetes_version)
VALUES ($1, $2, $3)
ON CONFLICT (cluster_id) DO UPDATE SET
    agent_version = EXCLUDED.agent_version,
    kubernetes_version = EXCLUDED.kubernetes_version,
    registered_at = now()
`, in.ClusterID, in.AgentVersion, in.KubernetesVersion)
	return err
}

func (s *Store) Heartbeat(ctx context.Context, in agent.Heartbeat) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE cluster_agents SET last_heartbeat_at = $2 WHERE cluster_id = $1
`, in.ClusterID, in.At)
	return err
}

func (s *Store) Desired(ctx context.Context, clusterID domain.ID) ([]agent.DesiredResource, error) {
	if s.db == nil {
		return nil, fmt.Errorf("postgres database is required")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT kind, resource_id, generation, spec, deletion_timestamp, finalizers
FROM desired_resources
WHERE cluster_id = $1
ORDER BY kind, resource_id
`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []agent.DesiredResource
	for rows.Next() {
		var item agent.DesiredResource
		var spec, finalizers []byte
		var deletionTimestamp sql.NullTime
		if err := rows.Scan(&item.Kind, &item.ID, &item.Generation, &spec, &deletionTimestamp, &finalizers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(spec, &item.Spec); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(finalizers, &item.Finalizers); err != nil {
			return nil, err
		}
		if deletionTimestamp.Valid {
			ts := deletionTimestamp.Time.UTC()
			item.DeletionTimestamp = &ts
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetDesired(ctx context.Context, clusterID domain.ID, kind string, resourceID domain.ID) (agent.DesiredResource, bool, error) {
	var item agent.DesiredResource
	var spec, finalizers []byte
	var deletionTimestamp sql.NullTime
	err := s.db.QueryRowContext(ctx, `
SELECT kind, resource_id, generation, spec, deletion_timestamp, finalizers
FROM desired_resources
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID).Scan(&item.Kind, &item.ID, &item.Generation, &spec, &deletionTimestamp, &finalizers)
	if err == sql.ErrNoRows {
		return agent.DesiredResource{}, false, nil
	}
	if err != nil {
		return agent.DesiredResource{}, false, err
	}
	if err := json.Unmarshal(spec, &item.Spec); err != nil {
		return agent.DesiredResource{}, false, err
	}
	if err := json.Unmarshal(finalizers, &item.Finalizers); err != nil {
		return agent.DesiredResource{}, false, err
	}
	if deletionTimestamp.Valid {
		ts := deletionTimestamp.Time.UTC()
		item.DeletionTimestamp = &ts
	}
	return item, true, nil
}

func (s *Store) LocateDesired(ctx context.Context, kind string, resourceID domain.ID) (domain.ID, agent.DesiredResource, bool, error) {
	if s.db == nil {
		return "", agent.DesiredResource{}, false, fmt.Errorf("postgres database is required")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT cluster_id, kind, resource_id, generation, spec, deletion_timestamp, finalizers
FROM desired_resources
WHERE kind = $1 AND resource_id = $2
ORDER BY cluster_id
LIMIT 2
`, kind, resourceID)
	if err != nil {
		return "", agent.DesiredResource{}, false, err
	}
	defer rows.Close()
	var clusterID domain.ID
	var item agent.DesiredResource
	matches := 0
	for rows.Next() {
		var spec, finalizers []byte
		var deletionTimestamp sql.NullTime
		if err := rows.Scan(&clusterID, &item.Kind, &item.ID, &item.Generation, &spec, &deletionTimestamp, &finalizers); err != nil {
			return "", agent.DesiredResource{}, false, err
		}
		if err := json.Unmarshal(spec, &item.Spec); err != nil {
			return "", agent.DesiredResource{}, false, err
		}
		if err := json.Unmarshal(finalizers, &item.Finalizers); err != nil {
			return "", agent.DesiredResource{}, false, err
		}
		item.DeletionTimestamp = nil
		if deletionTimestamp.Valid {
			ts := deletionTimestamp.Time.UTC()
			item.DeletionTimestamp = &ts
		}
		matches++
	}
	if err := rows.Err(); err != nil {
		return "", agent.DesiredResource{}, false, err
	}
	if matches > 1 {
		return "", agent.DesiredResource{}, false, agentstore.ErrIdentityConflict
	}
	if matches == 0 {
		return "", agent.DesiredResource{}, false, nil
	}
	return clusterID, item, true, nil
}

func (s *Store) GetObservation(ctx context.Context, clusterID domain.ID, kind string, resourceID domain.ID) (agent.Observation, bool, error) {
	var item agent.Observation
	var conditions, evidence []byte
	err := s.db.QueryRowContext(ctx, `
SELECT kind, resource_id, observed_generation, lease_owner, conditions, evidence_refs
FROM resource_observations
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID).Scan(&item.Kind, &item.ID, &item.ObservedGeneration, &item.LeaseOwner, &conditions, &evidence)
	if err == sql.ErrNoRows {
		return agent.Observation{}, false, nil
	}
	if err != nil {
		return agent.Observation{}, false, err
	}
	if err := json.Unmarshal(conditions, &item.Conditions); err != nil {
		return agent.Observation{}, false, err
	}
	if err := json.Unmarshal(evidence, &item.EvidenceRefs); err != nil {
		return agent.Observation{}, false, err
	}
	return item, true, nil
}

func (s *Store) UpsertDesired(
	ctx context.Context,
	clusterID domain.ID,
	in agent.DesiredResource,
) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertDesiredTx(ctx, tx, clusterID, in); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateWorkloadDesired(
	ctx context.Context,
	clusterID domain.ID,
	poolID domain.ID,
	acceleratorClass string,
	in agent.DesiredResource,
) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Workload identity is globally canonical. Serialize competing creates even
	// before a row exists so two clusters cannot race through LocateDesired.
	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"Workload/"+string(in.ID),
	); err != nil {
		return err
	}

	incomingSpec, err := json.Marshal(in.Spec)
	if err != nil {
		return fmt.Errorf("marshal desired spec: %w", err)
	}
	var existingClusterID domain.ID
	var existingGeneration int64
	var existingSpecMatches bool
	var existingDeletion sql.NullTime
	existingErr := tx.QueryRowContext(ctx, `
SELECT cluster_id, generation, spec = $2::jsonb, deletion_timestamp
FROM desired_resources
WHERE kind = 'Workload' AND resource_id = $1
ORDER BY cluster_id
LIMIT 1
FOR UPDATE
`, in.ID, incomingSpec).Scan(
		&existingClusterID,
		&existingGeneration,
		&existingSpecMatches,
		&existingDeletion,
	)
	if existingErr != nil && existingErr != sql.ErrNoRows {
		return existingErr
	}
	if existingErr == nil {
		if existingClusterID != clusterID {
			return agentstore.ErrIdentityConflict
		}
		if existingDeletion.Valid {
			return agentstore.ErrDesiredNotDeleting
		}
		if existingGeneration != in.Generation || !existingSpecMatches {
			return agentstore.ErrStaleGeneration
		}
		// Lost-ACK/idempotent replay: do not re-check parent lifecycle. The
		// original workload is already canonical and may be blocking pool delete.
		return tx.Commit()
	}

	var poolSpec []byte
	var deletionTimestamp sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT spec, deletion_timestamp
FROM desired_resources
WHERE cluster_id = $1 AND kind = 'ComputePool' AND resource_id = $2
FOR UPDATE
`, clusterID, poolID).Scan(&poolSpec, &deletionTimestamp)
	if err == sql.ErrNoRows {
		return agentstore.ErrComputePoolNotFound
	}
	if err != nil {
		return err
	}
	if deletionTimestamp.Valid {
		return agentstore.ErrComputePoolDeleting
	}

	var projected struct {
		AcceleratorBindings []domain.AcceleratorBinding `json:"acceleratorBindings"`
	}
	if err := json.Unmarshal(poolSpec, &projected); err != nil {
		return fmt.Errorf("decode compute pool desired spec: %w", err)
	}
	bindingFound := false
	for _, binding := range projected.AcceleratorBindings {
		if binding.Class == acceleratorClass {
			bindingFound = true
			break
		}
	}
	if !bindingFound {
		return agentstore.ErrAcceleratorBindingNotFound
	}

	if err := s.upsertDesiredTx(ctx, tx, clusterID, in); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) upsertDesiredTx(
	ctx context.Context,
	tx *sql.Tx,
	clusterID domain.ID,
	in agent.DesiredResource,
) error {
	var tombstoneGeneration int64
	tombstoneErr := tx.QueryRowContext(ctx, `
SELECT generation
FROM deletion_tombstones
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, in.Kind, in.ID).Scan(&tombstoneGeneration)
	if tombstoneErr != nil && tombstoneErr != sql.ErrNoRows {
		return tombstoneErr
	}
	if tombstoneErr == nil && in.Generation <= tombstoneGeneration {
		return agentstore.ErrStaleGeneration
	}

	spec, err := json.Marshal(in.Spec)
	if err != nil {
		return fmt.Errorf("marshal desired spec: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO desired_resources (cluster_id, kind, resource_id, generation, spec)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET
    generation = EXCLUDED.generation,
    spec = EXCLUDED.spec,
    updated_at = CASE
        WHEN desired_resources.generation < EXCLUDED.generation THEN now()
        ELSE desired_resources.updated_at
    END
WHERE desired_resources.generation < EXCLUDED.generation
   OR (
       desired_resources.generation = EXCLUDED.generation
       AND desired_resources.spec = EXCLUDED.spec
   )
`, clusterID, in.Kind, in.ID, in.Generation, spec)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return agentstore.ErrStaleGeneration
	}

	if tombstoneErr == nil {
		if _, err := tx.ExecContext(ctx, `
DELETE FROM deletion_tombstones
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, in.Kind, in.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) MarkDesiredDeleting(ctx context.Context, clusterID domain.ID, kind string, resourceID domain.ID, at time.Time) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE desired_resources
SET deletion_timestamp = COALESCE(deletion_timestamp, $4),
    finalizers = CASE
        WHEN finalizers ? $5 THEN finalizers
        ELSE finalizers || to_jsonb($5::text)
    END,
    updated_at = now()
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID, at.UTC(), agentstore.ProviderCleanupFinalizer)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return agentstore.ErrDesiredNotFound
	}
	return nil
}

func (s *Store) FinalizeDesired(
	ctx context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	generation int64,
) error {
	return s.finalizeDesired(
		ctx,
		clusterID,
		kind,
		resourceID,
		generation,
		"",
		false,
	)
}

func (s *Store) FinalizeDesiredOwned(
	ctx context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	generation int64,
	owner string,
) error {
	if owner == "" {
		return agentstore.ErrLeaseLost
	}
	return s.finalizeDesired(
		ctx,
		clusterID,
		kind,
		resourceID,
		generation,
		owner,
		true,
	)
}

func (s *Store) finalizeDesired(
	ctx context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	generation int64,
	owner string,
	requireLease bool,
) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if requireLease {
		var finalizedGeneration int64
		finalizedErr := tx.QueryRowContext(ctx, `
SELECT generation
FROM deletion_tombstones
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, kind, resourceID).Scan(&finalizedGeneration)
		if finalizedErr != nil && finalizedErr != sql.ErrNoRows {
			return finalizedErr
		}
		if finalizedErr == nil {
			switch {
			case generation == finalizedGeneration:
				return tx.Commit()
			case generation < finalizedGeneration:
				return agentstore.ErrStaleGeneration
			default:
				return agentstore.ErrDesiredNotFound
			}
		}

		var currentOwner string
		var leaseValid bool
		err = tx.QueryRowContext(ctx, `
SELECT owner, lease_until > now()
FROM reconcile_leases
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, kind, resourceID).Scan(&currentOwner, &leaseValid)
		if err == sql.ErrNoRows {
			return agentstore.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		if currentOwner != owner || !leaseValid {
			return agentstore.ErrLeaseLost
		}
	}

	var currentGeneration int64
	var deletionTimestamp sql.NullTime
	var finalizers []byte
	err = tx.QueryRowContext(ctx, `
SELECT generation, deletion_timestamp, finalizers
FROM desired_resources
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, kind, resourceID).Scan(&currentGeneration, &deletionTimestamp, &finalizers)
	if err == sql.ErrNoRows {
		return agentstore.ErrDesiredNotFound
	}
	if err != nil {
		return err
	}
	if currentGeneration != generation {
		return agentstore.ErrStaleGeneration
	}
	var currentFinalizers []string
	if err := json.Unmarshal(finalizers, &currentFinalizers); err != nil {
		return err
	}
	hasCleanupFinalizer := false
	for _, finalizer := range currentFinalizers {
		if finalizer == agentstore.ProviderCleanupFinalizer {
			hasCleanupFinalizer = true
			break
		}
	}
	if !deletionTimestamp.Valid || !hasCleanupFinalizer {
		return agentstore.ErrDesiredNotDeleting
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO deletion_tombstones (cluster_id, kind, resource_id, generation)
VALUES ($1, $2, $3, $4)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET
    generation = GREATEST(deletion_tombstones.generation, EXCLUDED.generation),
    finalized_at = now()
`, clusterID, kind, resourceID, generation); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM resource_observations
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM reconcile_leases
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM desired_resources
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3 AND generation = $4
`, clusterID, kind, resourceID, generation); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinalizedGeneration(ctx context.Context, clusterID domain.ID, kind string, resourceID domain.ID) (int64, bool, error) {
	if s.db == nil {
		return 0, false, fmt.Errorf("postgres database is required")
	}
	var generation int64
	err := s.db.QueryRowContext(ctx, `
SELECT generation
FROM deletion_tombstones
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID).Scan(&generation)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return generation, true, nil
}

func (s *Store) Report(ctx context.Context, clusterID domain.ID, observations []agent.Observation) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, item := range observations {
		if item.LeaseOwner != "" {
			var currentOwner string
			var leaseValid bool
			err := tx.QueryRowContext(ctx, `
SELECT owner, lease_until > now()
FROM reconcile_leases
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, item.Kind, item.ID).Scan(&currentOwner, &leaseValid)
			if err == sql.ErrNoRows {
				return agentstore.ErrLeaseLost
			}
			if err != nil {
				return err
			}
			if currentOwner != item.LeaseOwner || !leaseValid {
				return agentstore.ErrLeaseLost
			}
		}

		var desiredGeneration int64
		err := tx.QueryRowContext(ctx, `
SELECT generation
FROM desired_resources
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, item.Kind, item.ID).Scan(&desiredGeneration)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows {
			// Observations are subordinate to desired state. Do not persist
			// a future/stale observation while the canonical resource does
			// not exist, because a later recreate may use that generation.
			continue
		}
		if item.ObservedGeneration != desiredGeneration {
			continue
		}

		var previousJSON []byte
		var previousGeneration int64
		err = tx.QueryRowContext(ctx, `
SELECT observed_generation, conditions
FROM resource_observations
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
FOR UPDATE
`, clusterID, item.Kind, item.ID).Scan(&previousGeneration, &previousJSON)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if item.ObservedGeneration < previousGeneration {
				continue
			}
			var previous []domain.Condition
			if err := json.Unmarshal(previousJSON, &previous); err != nil {
				return err
			}
			item.Conditions = agentstore.MergeConditions(previous, item.Conditions)
		}

		conditions, err := json.Marshal(item.Conditions)
		if err != nil {
			return err
		}
		evidence, err := json.Marshal(item.EvidenceRefs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO resource_observations
    (cluster_id, kind, resource_id, observed_generation, lease_owner, conditions, evidence_refs)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET
    observed_generation = EXCLUDED.observed_generation,
    lease_owner = EXCLUDED.lease_owner,
    conditions = EXCLUDED.conditions,
    evidence_refs = EXCLUDED.evidence_refs,
    observed_at = now()
`, clusterID, item.Kind, item.ID, item.ObservedGeneration, item.LeaseOwner, conditions, evidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}
