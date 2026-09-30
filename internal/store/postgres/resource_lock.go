package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"sort"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func resourceLockKey(clusterID domain.ID, kind string, resourceID domain.ID) int64 {
	identity, _ := json.Marshal([]string{"resource-lifecycle", string(clusterID), kind, string(resourceID)})
	digest := sha256.Sum256(identity)
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

// Lock the identity even when desired/observation rows do not exist yet.
// All writers acquire this lock before row locks, at READ COMMITTED isolation.
func lockResourceTx(ctx context.Context, tx *sql.Tx, clusterID domain.ID, kind string, resourceID domain.ID) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, resourceLockKey(clusterID, kind, resourceID))
	return err
}

func lockObservationBatchTx(ctx context.Context, tx *sql.Tx, clusterID domain.ID, observations []agent.Observation) error {
	keys := make([]int64, 0, len(observations))
	for _, item := range observations {
		keys = append(keys, resourceLockKey(clusterID, item.Kind, item.ID))
	}
	// Sort actual lock keys (including hash collisions), so reversed batches
	// cannot create a lock-order cycle. Collisions only serialize more work.
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for i, key := range keys {
		if i > 0 && key == keys[i-1] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return err
		}
	}
	return nil
}
