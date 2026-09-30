package postgres

import (
	"context"
	"database/sql"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

// lockDesiredLifecycleTx serializes lifecycle mutations for one canonical
// desired-resource identity. Row locks alone are insufficient when the desired
// row is absent during finalize -> recreate transitions, so use a transaction-
// scoped advisory lock that exists independently of row presence.
func lockDesiredLifecycleTx(
	ctx context.Context,
	tx *sql.Tx,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
) error {
	_, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"desired/"+string(clusterID)+"/"+kind+"/"+string(resourceID),
	)
	return err
}
