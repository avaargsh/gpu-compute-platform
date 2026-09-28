package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
)

func (s *Store) ResolveProject(ctx context.Context, projectID domain.ID) (httpapi.Placement, error) {
	if s.db == nil {
		return httpapi.Placement{}, fmt.Errorf("postgres database is required")
	}
	var out httpapi.Placement
	err := s.db.QueryRowContext(ctx, `
SELECT cluster_id, namespace
FROM project_bindings
WHERE project_id = $1
`, projectID).Scan(&out.ClusterID, &out.Namespace)
	if err == sql.ErrNoRows {
		return httpapi.Placement{}, fmt.Errorf("project binding not found: %s", projectID)
	}
	return out, err
}

func (s *Store) ResolvePool(ctx context.Context, poolID domain.ID) (httpapi.Placement, error) {
	if s.db == nil {
		return httpapi.Placement{}, fmt.Errorf("postgres database is required")
	}
	var out httpapi.Placement
	err := s.db.QueryRowContext(ctx, `
SELECT cluster_id, provider
FROM cluster_bindings
WHERE pool_id = $1
`, poolID).Scan(&out.ClusterID, &out.Provider)
	if err == sql.ErrNoRows {
		return httpapi.Placement{}, fmt.Errorf("cluster binding not found: %s", poolID)
	}
	return out, err
}

func (s *Store) UpsertProjectBinding(ctx context.Context, in domain.ProjectBinding) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO project_bindings (project_id, cluster_id, namespace, generation)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id) DO UPDATE SET
    cluster_id = EXCLUDED.cluster_id,
    namespace = EXCLUDED.namespace,
    generation = EXCLUDED.generation,
    updated_at = now()
`, in.ProjectID, in.ClusterID, in.Namespace, in.Metadata.Generation)
	return err
}

func (s *Store) UpsertClusterBinding(ctx context.Context, in domain.ClusterBinding) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cluster_bindings (pool_id, cluster_id, provider, generation)
VALUES ($1, $2, $3, $4)
ON CONFLICT (pool_id) DO UPDATE SET
    cluster_id = EXCLUDED.cluster_id,
    provider = EXCLUDED.provider,
    generation = EXCLUDED.generation,
    updated_at = now()
`, in.PoolID, in.ClusterID, in.Provider, in.Metadata.Generation)
	return err
}
