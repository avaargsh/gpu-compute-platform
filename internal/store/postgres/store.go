package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
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
SELECT kind, resource_id, generation, spec
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
		var spec []byte
		if err := rows.Scan(&item.Kind, &item.ID, &item.Generation, &spec); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(spec, &item.Spec); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}


func (s *Store) UpsertDesired(ctx context.Context, clusterID domain.ID, in agent.DesiredResource) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	spec, err := json.Marshal(in.Spec)
	if err != nil {
		return fmt.Errorf("marshal desired spec: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO desired_resources (cluster_id, kind, resource_id, generation, spec)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET
    generation = EXCLUDED.generation,
    spec = EXCLUDED.spec,
    updated_at = now()
`, clusterID, in.Kind, in.ID, in.Generation, spec)
	return err
}

func (s *Store) DeleteDesired(ctx context.Context, clusterID domain.ID, kind string, resourceID domain.ID) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	_, err := s.db.ExecContext(ctx, `
DELETE FROM desired_resources WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, kind, resourceID)
	return err
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
    (cluster_id, kind, resource_id, observed_generation, conditions, evidence_refs)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET
    observed_generation = EXCLUDED.observed_generation,
    conditions = EXCLUDED.conditions,
    evidence_refs = EXCLUDED.evidence_refs,
    observed_at = now()
`, clusterID, item.Kind, item.ID, item.ObservedGeneration, conditions, evidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}
