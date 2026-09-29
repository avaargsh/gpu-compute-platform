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

	grant, err := store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 1)
	if err != nil || !grant.Claimed {
		t.Fatalf("initial claim: claimed=%t err=%v", grant.Claimed, err)
	}
	if grant.Owner != "worker-a" || grant.Epoch != 1 || grant.ExpiresAt.IsZero() {\n\t\tt.Fatalf("unexpected initial grant: %#v", grant)\n\t}\n\tvar initialEpoch int64
	if err := db.QueryRowContext(ctx, `SELECT epoch FROM reconcile_leases WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'`).Scan(&initialEpoch); err != nil {
		t.Fatal(err)
	}
	grant, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || grant.Claimed {
		t.Fatalf("competing owner must be fenced: claimed=%t err=%v", grant.Claimed, err)
	}
	grant, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 1)
	if err != nil || !grant.Claimed {
		t.Fatalf("same owner renew: claimed=%t err=%v", grant.Claimed, err)
	}
	var renewedEpoch int64
	if err := db.QueryRowContext(ctx, `SELECT epoch FROM reconcile_leases WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'`).Scan(&renewedEpoch); err != nil {
		t.Fatal(err)
	}
	if grant.Epoch != initialEpoch {\n\t\tt.Fatalf("renewed grant epoch=%d, want %d", grant.Epoch, initialEpoch)\n\t}\n\tif renewedEpoch != initialEpoch {
		t.Fatalf("same-owner renewal changed epoch: got=%d want=%d", renewedEpoch, initialEpoch)
	}

	if _, err := db.ExecContext(ctx, `
UPDATE reconcile_leases
SET lease_until = now() - interval '1 second'
WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'
`); err != nil {
		t.Fatal(err)
	}
	grant, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || !grant.Claimed {
		t.Fatalf("expired lease takeover: claimed=%t err=%v", grant.Claimed, err)
	}
	var takeoverEpoch int64
	if err := db.QueryRowContext(ctx, `SELECT epoch FROM reconcile_leases WHERE cluster_id = 'cluster-a' AND kind = 'Workload' AND resource_id = 'train-1'`).Scan(&takeoverEpoch); err != nil {
		t.Fatal(err)
	}
	if grant.Owner != "worker-b" || grant.Epoch != initialEpoch+1 {\n\t\tt.Fatalf("unexpected takeover grant: %#v", grant)\n\t}\n\tif takeoverEpoch != initialEpoch+1 {
		t.Fatalf("takeover epoch=%d, want %d", takeoverEpoch, initialEpoch+1)
	}

	if err := store.ReleaseReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a"); err != nil {
		t.Fatal(err)
	}
	grant, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 30)
	if err != nil || grant.Claimed {
		t.Fatalf("stale owner release must not clear new lease: claimed=%t err=%v", grant.Claimed, err)
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
