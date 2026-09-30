package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
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
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || claimed {
		t.Fatalf("competing owner must be fenced: claimed=%t err=%v", claimed, err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 1)
	if err != nil || !claimed {
		t.Fatalf("same owner renew: claimed=%t err=%v", claimed, err)
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

func TestPostgresLeaseTakeoverFencesStaleReportAndFinalize(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()

	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{
		Kind:       "Workload",
		ID:         "train-fenced",
		Generation: 7,
		Spec:       map[string]any{"image": "example/train:v7"},
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := store.ClaimReconcileLease(
		ctx, "cluster-a", "Workload", "train-fenced", "agent-a", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("agent-a claim: claimed=%t err=%v", claimed, err)
	}

	if err := store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind:               "Workload",
		ID:                 "train-fenced",
		ObservedGeneration: 7,
		LeaseOwner:         "agent-a",
		EvidenceRefs:       []string{"evidence://agent-a"},
	}}); err != nil {
		t.Fatalf("agent-a report while lease is live: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
UPDATE reconcile_leases
SET lease_until = now() - interval '1 second'
WHERE cluster_id = 'cluster-a'
  AND kind = 'Workload'
  AND resource_id = 'train-fenced'
`); err != nil {
		t.Fatal(err)
	}

	claimed, err = store.ClaimReconcileLease(
		ctx, "cluster-a", "Workload", "train-fenced", "agent-b", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("agent-b takeover: claimed=%t err=%v", claimed, err)
	}

	err = store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind:               "Workload",
		ID:                 "train-fenced",
		ObservedGeneration: 7,
		LeaseOwner:         "agent-a",
		EvidenceRefs:       []string{"evidence://stale-agent-a"},
	}})
	if !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale agent report err=%v, want ErrLeaseLost", err)
	}

	if err := store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind:               "Workload",
		ID:                 "train-fenced",
		ObservedGeneration: 7,
		LeaseOwner:         "agent-b",
		EvidenceRefs:       []string{"evidence://agent-b"},
	}}); err != nil {
		t.Fatalf("takeover owner report: %v", err)
	}

	observed, found, err := store.GetObservation(
		ctx, "cluster-a", "Workload", "train-fenced",
	)
	if err != nil || !found {
		t.Fatalf("get observation: found=%t err=%v", found, err)
	}
	if len(observed.EvidenceRefs) != 1 || observed.EvidenceRefs[0] != "evidence://agent-b" {
		t.Fatalf("stale writer changed evidence: %#v", observed.EvidenceRefs)
	}

	if err := store.MarkDesiredDeleting(
		ctx, "cluster-a", "Workload", "train-fenced", time.Now(),
	); err != nil {
		t.Fatal(err)
	}

	err = store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-fenced", 7, "agent-a",
	)
	if !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale agent finalize err=%v, want ErrLeaseLost", err)
	}

	if err := store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-fenced", 7, "agent-b",
	); err != nil {
		t.Fatalf("takeover owner finalize: %v", err)
	}
}
