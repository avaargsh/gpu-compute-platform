package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPostgresStaleLeaseCannotReportOrFinalizeAfterTakeover(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-a")
	resourceID := domain.ID("train-fenced")
	const generation int64 = 9

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	a, err := store.ClaimReconcileLease(ctx, clusterID, "Workload", resourceID, "worker-a", 120)
	if err != nil || !a.Claimed {
		t.Fatalf("claim A: %#v err=%v", a, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE reconcile_leases SET lease_until = now() - interval '1 second'
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, "Workload", resourceID); err != nil {
		t.Fatal(err)
	}
	b, err := store.ClaimReconcileLease(ctx, clusterID, "Workload", resourceID, "worker-b", 120)
	if err != nil || !b.Claimed || b.Epoch != a.Epoch+1 {
		t.Fatalf("takeover B: A=%#v B=%#v err=%v", a, b, err)
	}

	stale := agent.Observation{
		Kind: "Workload", ID: resourceID, ObservedGeneration: generation,
		LeaseOwner: a.Owner, LeaseEpoch: a.Epoch,
	}
	if err := store.Report(ctx, clusterID, []agent.Observation{stale}); !errors.Is(err, agentstore.ErrStaleReconcileLease) {
		t.Fatalf("stale report err=%v, want ErrStaleReconcileLease", err)
	}
	current := stale
	current.LeaseOwner, current.LeaseEpoch = b.Owner, b.Epoch
	if err := store.Report(ctx, clusterID, []agent.Observation{current}); err != nil {
		t.Fatalf("current owner report: %v", err)
	}

	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", resourceID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesired(ctx, clusterID, "Workload", resourceID, generation, a.Owner, a.Epoch); !errors.Is(err, agentstore.ErrStaleReconcileLease) {
		t.Fatalf("stale finalize err=%v, want ErrStaleReconcileLease", err)
	}
	if err := store.FinalizeDesired(ctx, clusterID, "Workload", resourceID, generation, b.Owner, b.Epoch); err != nil {
		t.Fatalf("current owner finalize: %v", err)
	}
}
