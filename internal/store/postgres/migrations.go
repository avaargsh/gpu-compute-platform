package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func (s *Store) CreatePlacementMigration(ctx context.Context, in domain.PlacementMigration) (domain.PlacementMigration, bool, error) {
	if s.db == nil {
		return domain.PlacementMigration{}, false, fmt.Errorf("postgres database is required")
	}
	if !in.ValidateRequest() || in.Metadata.Generation <= 0 {
		return domain.PlacementMigration{}, false, fmt.Errorf("invalid placement migration")
	}
	conditions, err := json.Marshal(in.Conditions)
	if err != nil {
		return domain.PlacementMigration{}, false, err
	}
	evidence, err := json.Marshal(in.EvidenceRefs)
	if err != nil {
		return domain.PlacementMigration{}, false, err
	}

	result, err := s.db.ExecContext(ctx, `
INSERT INTO placement_migrations
    (migration_id, pool_id, source_cluster_id, target_cluster_id, generation, phase, conditions, evidence_refs)
SELECT $1, $2, $3, $4, $5, $6, $7, $8
FROM cluster_bindings
WHERE pool_id = $2 AND cluster_id = $3
ON CONFLICT (migration_id) DO NOTHING
`, in.Metadata.ID, in.PoolID, in.SourceClusterID, in.TargetClusterID, in.Metadata.Generation, in.Phase, conditions, evidence)
	if err != nil {
		return domain.PlacementMigration{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.PlacementMigration{}, false, err
	}
	if affected > 0 {
		created, err := s.GetPlacementMigration(ctx, in.PoolID, in.Metadata.ID)
		return created, true, err
	}

	var currentCluster domain.ID
	err = s.db.QueryRowContext(ctx, `
SELECT cluster_id FROM cluster_bindings WHERE pool_id = $1
`, in.PoolID).Scan(&currentCluster)
	if err == sql.ErrNoRows {
		return domain.PlacementMigration{}, false, fmt.Errorf("cluster binding not found: %s", in.PoolID)
	}
	if err != nil {
		return domain.PlacementMigration{}, false, err
	}
	if currentCluster != in.SourceClusterID {
		return domain.PlacementMigration{}, false, agentstore.ErrPlacementSourceMismatch
	}

	existing, err := s.getPlacementMigration(ctx, in.PoolID, in.Metadata.ID)
	if err != nil {
		return domain.PlacementMigration{}, false, err
	}
	if existing.Metadata.Generation == in.Metadata.Generation &&
		existing.SourceClusterID == in.SourceClusterID &&
		existing.TargetClusterID == in.TargetClusterID {
		return existing, false, nil
	}
	return domain.PlacementMigration{}, false, agentstore.ErrPlacementMigrationConflict
}

func (s *Store) GetPlacementMigration(ctx context.Context, poolID, migrationID domain.ID) (domain.PlacementMigration, error) {
	return s.getPlacementMigration(ctx, poolID, migrationID)
}

func (s *Store) getPlacementMigration(ctx context.Context, poolID, migrationID domain.ID) (domain.PlacementMigration, error) {
	var out domain.PlacementMigration
	var conditions, evidence []byte
	err := s.db.QueryRowContext(ctx, `
SELECT migration_id, pool_id, source_cluster_id, target_cluster_id, generation, phase,
       conditions, evidence_refs, created_at, updated_at
FROM placement_migrations
WHERE pool_id = $1 AND migration_id = $2
`, poolID, migrationID).Scan(
		&out.Metadata.ID,
		&out.PoolID,
		&out.SourceClusterID,
		&out.TargetClusterID,
		&out.Metadata.Generation,
		&out.Phase,
		&conditions,
		&evidence,
		&out.Metadata.CreatedAt,
		&out.Metadata.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	if err := json.Unmarshal(conditions, &out.Conditions); err != nil {
		return domain.PlacementMigration{}, err
	}
	if err := json.Unmarshal(evidence, &out.EvidenceRefs); err != nil {
		return domain.PlacementMigration{}, err
	}
	return out, nil
}

func (s *Store) UpdatePlacementMigration(ctx context.Context, poolID, migrationID domain.ID, phase domain.PlacementMigrationPhase, conditions []domain.Condition, evidenceRefs []string) (domain.PlacementMigration, error) {
	if s.db == nil {
		return domain.PlacementMigration{}, fmt.Errorf("postgres database is required")
	}
	current, err := s.getPlacementMigration(ctx, poolID, migrationID)
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	if !current.Phase.CanTransitionTo(phase) {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationTransition
	}
	conditionsJSON, err := json.Marshal(conditions)
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	evidenceJSON, err := json.Marshal(evidenceRefs)
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE placement_migrations
SET phase = $3, conditions = $4, evidence_refs = $5, updated_at = now()
WHERE pool_id = $1 AND migration_id = $2 AND phase = $6
`, poolID, migrationID, phase, conditionsJSON, evidenceJSON, current.Phase)
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	if affected == 0 {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationTransition
	}
	return s.getPlacementMigration(ctx, poolID, migrationID)
}

func (s *Store) CutoverPlacementMigration(ctx context.Context, poolID, migrationID domain.ID) (out domain.PlacementMigration, err error) {
	if s.db == nil {
		return domain.PlacementMigration{}, fmt.Errorf("postgres database is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var phase domain.PlacementMigrationPhase
	var sourceClusterID, targetClusterID domain.ID
	err = tx.QueryRowContext(ctx, `
SELECT phase, source_cluster_id, target_cluster_id
FROM placement_migrations
WHERE pool_id = $1 AND migration_id = $2
FOR UPDATE
`, poolID, migrationID).Scan(&phase, &sourceClusterID, &targetClusterID)
	if err == sql.ErrNoRows {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	if phase != domain.PlacementMigrationReadyToCutover {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationTransition
	}

	var currentClusterID domain.ID
	err = tx.QueryRowContext(ctx, `
SELECT cluster_id
FROM cluster_bindings
WHERE pool_id = $1
FOR UPDATE
`, poolID).Scan(&currentClusterID)
	if err != nil {
		return domain.PlacementMigration{}, err
	}
	if currentClusterID != sourceClusterID {
		return domain.PlacementMigration{}, agentstore.ErrPlacementSourceMismatch
	}

	if _, err = tx.ExecContext(ctx, `
UPDATE cluster_bindings
SET cluster_id = $2, generation = generation + 1, updated_at = now()
WHERE pool_id = $1
`, poolID, targetClusterID); err != nil {
		return domain.PlacementMigration{}, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE placement_migrations
SET phase = $3, updated_at = now()
WHERE pool_id = $1 AND migration_id = $2
`, poolID, migrationID, domain.PlacementMigrationCutover); err != nil {
		return domain.PlacementMigration{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.PlacementMigration{}, err
	}
	return s.getPlacementMigration(ctx, poolID, migrationID)
}
