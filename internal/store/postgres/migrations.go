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
		return s.GetPlacementMigration(ctx, in.PoolID, in.Metadata.ID, true)
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
