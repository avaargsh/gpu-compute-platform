package postgres

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func (s *Store) ClaimReconcileLease(
	ctx context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	owner string,
	ttlSeconds int64,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("postgres database is required")
	}
	if clusterID == "" || kind == "" || resourceID == "" || owner == "" {
		return false, fmt.Errorf("reconcile lease identity is required")
	}
	if ttlSeconds <= 0 {
		return false, fmt.Errorf("reconcile lease ttl must be positive")
	}

	result, err := s.db.ExecContext(ctx, `
INSERT INTO reconcile_leases
    (cluster_id, kind, resource_id, owner, lease_until, epoch)
VALUES ($1, $2, $3, $4, now() + ($5 * interval '1 second'), 1)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET
    owner = EXCLUDED.owner,
    lease_until = now() + ($5 * interval '1 second'),
    epoch = CASE
        WHEN reconcile_leases.owner = EXCLUDED.owner THEN reconcile_leases.epoch
        ELSE reconcile_leases.epoch + 1
    END,
    updated_at = now()
WHERE reconcile_leases.lease_until <= now()
   OR reconcile_leases.owner = EXCLUDED.owner
`, clusterID, kind, resourceID, owner, ttlSeconds)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (s *Store) ReleaseReconcileLease(
	ctx context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	owner string,
) error {
	if s.db == nil {
		return fmt.Errorf("postgres database is required")
	}
	if owner == "" {
		return fmt.Errorf("reconcile lease owner is required")
	}
	_, err := s.db.ExecContext(ctx, `
DELETE FROM reconcile_leases
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3 AND owner = $4
`, clusterID, kind, resourceID, owner)
	return err
}
