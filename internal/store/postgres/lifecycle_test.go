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

func TestPostgresFinalizeDesiredAtomicallyCleansRuntimeState(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-a")
	resourceID := domain.ID("train-finalize")
	const generation int64 = 4

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation, Spec: map[string]any{"image": "example/train:latest"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: resourceID, ObservedGeneration: generation,
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "Deleting"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", resourceID, time.Now()); err != nil {
		t.Fatal(err)
	}
	grant, err := store.ClaimReconcileLease(ctx, clusterID, "Workload", resourceID, "worker-a", 120)
	if err != nil || !grant.Claimed {
		t.Fatalf("claim before finalize: claimed=%t err=%v", grant.Claimed, err)
	}

	if err := store.FinalizeDesired(ctx, clusterID, "Workload", resourceID, generation); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := store.GetDesired(ctx, clusterID, "Workload", resourceID); err != nil || ok {
		t.Fatalf("desired must be deleted: ok=%t err=%v", ok, err)
	}
	if _, ok, err := store.GetObservation(ctx, clusterID, "Workload", resourceID); err != nil || ok {
		t.Fatalf("observation must be deleted: ok=%t err=%v", ok, err)
	}
	var leaseCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM reconcile_leases
WHERE cluster_id = $1 AND kind = $2 AND resource_id = $3
`, clusterID, "Workload", resourceID).Scan(&leaseCount); err != nil {
		t.Fatal(err)
	}
	if leaseCount != 0 {
		t.Fatalf("lease must be deleted, count=%d", leaseCount)
	}
	finalizedGeneration, ok, err := store.FinalizedGeneration(ctx, clusterID, "Workload", resourceID)
	if err != nil || !ok || finalizedGeneration != generation {
		t.Fatalf("unexpected tombstone: generation=%d ok=%t err=%v", finalizedGeneration, ok, err)
	}
}

func TestPostgresTombstoneFencesOldGenerationAndAllowsNewerRecreate(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-a")
	resourceID := domain.ID("train-recreate")
	const generation int64 = 7

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", resourceID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesired(ctx, clusterID, "Workload", resourceID, generation); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation, Spec: map[string]any{},
	}); !errors.Is(err, agentstore.ErrStaleGeneration) {
		t.Fatalf("recreate at tombstoned generation err=%v, want ErrStaleGeneration", err)
	}
	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation + 1, Spec: map[string]any{},
	}); err != nil {
		t.Fatalf("newer generation must recreate resource: %v", err)
	}
	if _, ok, err := store.FinalizedGeneration(ctx, clusterID, "Workload", resourceID); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("newer generation recreate must consume the old tombstone")
	}
}
