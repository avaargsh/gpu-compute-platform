package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func openContractDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"reconcile_leases", "resource_observations", "deletion_tombstones", "desired_resources"} {
		if _, err := db.ExecContext(ctx, "TRUNCATE TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestPostgresReconcileLeaseOwnershipAndExpiry(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()

	claimed, err := store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 1)
	if err != nil || !claimed {
		t.Fatalf("initial claim: claimed=%t err=%v", claimed, err)
	}
	var initialEpoch int64
	if err := db.QueryRowContext(ctx, `SELECT epoch FROM reconcile_leases WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'`).Scan(&initialEpoch); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || claimed {
		t.Fatalf("competing owner must be fenced: claimed=%t err=%v", claimed, err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 1)
	if err != nil || !claimed {
		t.Fatalf("same owner renew: claimed=%t err=%v", claimed, err)
	}
	var renewedEpoch int64
	if err := db.QueryRowContext(ctx, `SELECT epoch FROM reconcile_leases WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'`).Scan(&renewedEpoch); err != nil {
		t.Fatal(err)
	}
	if renewedEpoch != initialEpoch {
		t.Fatalf("same-owner renewal changed epoch: got=%d want=%d", renewedEpoch, initialEpoch)
	}

	if _, err := db.ExecContext(ctx, `
UPDATE reconcile_leases
SET lease_until = now() - interval '1 second'
WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'
`); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || !claimed {
		t.Fatalf("expired lease takeover: claimed=%t err=%v", claimed, err)
	}
	var takeoverEpoch int64
	if err := db.QueryRowContext(ctx, `SELECT epoch FROM reconcile_leases WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'`).Scan(&takeoverEpoch); err != nil {
		t.Fatal(err)
	}
	if takeoverEpoch != initialEpoch+1 {
		t.Fatalf("takeover epoch=%d, want %d", takeoverEpoch, initialEpoch+1)
	}

	if err := store.ReleaseReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a"); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 30)
	if err != nil || claimed {
		t.Fatalf("stale owner release must not clear new lease: claimed=%t err=%v", claimed, err)
	}
}

func TestPostgresLeaseInputValidation(t *testing.T) {
	store := New(openContractDB(t))
	ctx := context.Background()
	if _, err := store.ClaimReconcileLease(ctx, "", "Workload", "train-1", "worker-a", 30); err == nil {
		t.Fatal("missing identity must fail")
	}
	if _, err := store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 0); err == nil {
		t.Fatal("non-positive ttl must fail")
	}
	if err := store.ReleaseReconcileLease(ctx, "cluster-a", "Workload", "train-1", ""); err == nil {
		t.Fatal("missing owner must fail")
	}
}
