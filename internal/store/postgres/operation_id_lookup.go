package postgres

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

// attachOriginalOperationIDs is a conservative, optional correlation lookup.
// The Management schema may not exist in legacy and isolated Store deployments.
// Missing or non-Management Desired always carries NO operation ID. Having an
// ID is not an authorization grant or evidence of a Kubernetes CREATE response.
func (s *Store) attachOriginalOperationIDs(ctx context.Context, clusterID domain.ID, items []agent.DesiredResource) error {
	if len(items) == 0 {
		return nil
	}
	var auditInstalled bool
	if err := s.db.QueryRowContext(ctx, `SELECT to_regclass('public.management_operation_audit') IS NOT NULL`).Scan(&auditInstalled); err != nil {
		return fmt.Errorf("inspect Management audit presence: %w", err)
	}
	if !auditInstalled {
		return nil
	}
	for i := range items {
		item := &items[i]
		if item.Kind != "Workload" || item.Generation < 1 || item.DeletionTimestamp != nil {
			continue
		}
		project, ok := item.Spec["projectID"].(string)
		if !ok || project == "" {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
SELECT request_id FROM public.management_operation_audit
WHERE cluster_id=$1 AND project_id=$2 AND kind='Workload'
  AND resource_id=$3 AND action='create' AND expected_generation=0
  AND resulting_generation=$4 AND request_digest ~ '^[0-9a-f]{64}$'
  AND request_digest <> repeat('0',64)
ORDER BY request_id LIMIT 2
`, clusterID, project, item.ID, item.Generation)
		if err != nil {
			return fmt.Errorf("read original Management operation: %w", err)
		}
		count := 0
		var operationID string
		for rows.Next() {
			if err := rows.Scan(&operationID); err != nil {
				rows.Close()
				return err
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count > 1 {
			// Never attribute an ambiguous duplicate audit to either caller.
			return fmt.Errorf("ambiguous original Management CREATE audit for Workload %s", item.ID)
		}
		if count == 1 {
			item.OperationID = operationID
		}
	}
	return nil
}
