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
	claimed, err := store.ClaimReconcileLease(ctx, clusterID, "Workload", resourceID, "worker-a", 120)
	if err != nil || !claimed {
		t.Fatalf("claim before finalize: claimed=%t err=%v", claimed, err)
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

func TestPostgresUpsertSameGenerationIsIdempotentButImmutable(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-a")
	resourceID := domain.ID("pool-generation-fence")

	original := agent.DesiredResource{
		Kind:       "ComputePool",
		ID:         resourceID,
		Generation: 5,
		Spec: map[string]any{
			"namespace": "project-1",
			"quota":     8,
		},
	}
	if err := store.UpsertDesired(ctx, clusterID, original); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDesired(ctx, clusterID, original); err != nil {
		t.Fatalf("identical replay must be accepted: %v", err)
	}

	mutated := original
	mutated.Spec = map[string]any{
		"namespace": "project-1",
		"quota":     16,
	}
	if err := store.UpsertDesired(ctx, clusterID, mutated); !errors.Is(err, agentstore.ErrStaleGeneration) {
		t.Fatalf("same-generation mutation err=%v, want ErrStaleGeneration", err)
	}

	got, found, err := store.GetDesired(ctx, clusterID, "ComputePool", resourceID)
	if err != nil || !found {
		t.Fatalf("desired missing: found=%t err=%v", found, err)
	}
	if got.Spec["quota"] != float64(8) {
		t.Fatalf("same-generation mutation changed desired spec: %#v", got.Spec)
	}
}

func TestPostgresFailedRecreateDoesNotConsumeTombstone(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-a")
	resourceID := domain.ID("train-tombstone-atomic")
	const generation int64 = 11

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

	err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind:       "Workload",
		ID:         resourceID,
		Generation: generation + 1,
		Spec: map[string]any{
			"invalid": func() {},
		},
	})
	if err == nil {
		t.Fatal("invalid desired spec must fail")
	}

	finalizedGeneration, found, err := store.FinalizedGeneration(
		ctx, clusterID, "Workload", resourceID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || finalizedGeneration != generation {
		t.Fatalf(
			"failed recreate consumed tombstone: generation=%d found=%t",
			finalizedGeneration,
			found,
		)
	}

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation, Spec: map[string]any{},
	}); !errors.Is(err, agentstore.ErrStaleGeneration) {
		t.Fatalf("tombstone fence lost after failed recreate: %v", err)
	}
}

func TestPostgresReportCannotPreseedFutureRecreate(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-a")
	resourceID := domain.ID("train-observation-fence")
	const generation int64 = 4

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: resourceID, ObservedGeneration: generation,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", Reason: "Current"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx,
		clusterID,
		"Workload",
		resourceID,
		time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesired(
		ctx,
		clusterID,
		"Workload",
		resourceID,
		generation,
	); err != nil {
		t.Fatal(err)
	}

	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: resourceID, ObservedGeneration: generation + 1,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", Reason: "Future"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetObservation(
		ctx,
		clusterID,
		"Workload",
		resourceID,
	); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("future observation without desired state must not persist")
	}

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation + 1, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetObservation(
		ctx,
		clusterID,
		"Workload",
		resourceID,
	); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("new desired generation inherited a pre-intent observation")
	}

	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: resourceID, ObservedGeneration: generation + 1,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", Reason: "Reconciled"}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.GetObservation(
		ctx,
		clusterID,
		"Workload",
		resourceID,
	)
	if err != nil || !found {
		t.Fatalf("current observation missing: found=%t err=%v", found, err)
	}
	if got.ObservedGeneration != generation+1 {
		t.Fatalf("observed generation=%d, want %d", got.ObservedGeneration, generation+1)
	}
}

func TestPostgresCreateWorkloadDesiredRejectsConcurrentPoolDelete(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-parent-race")
	poolID := domain.ID("pool-parent-race")
	workloadID := domain.ID("train-parent-race")

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind:       "ComputePool",
		ID:         poolID,
		Generation: 1,
		Spec: map[string]any{
			"acceleratorBindings": []domain.AcceleratorBinding{{
				Class: "h100", ResourceName: "nvidia.com/gpu", Flavor: "h100",
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE desired_resources
SET deletion_timestamp = now()
WHERE cluster_id = $1 AND kind = 'ComputePool' AND resource_id = $2
`, clusterID, poolID); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- store.CreateWorkloadDesired(
			ctx,
			clusterID,
			poolID,
			"h100",
			agent.DesiredResource{
				Kind: "Workload", ID: workloadID, Generation: 1,
				Spec: map[string]any{"poolID": poolID},
			},
		)
	}()

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, agentstore.ErrComputePoolDeleting) {
			t.Fatalf("create err=%v, want ErrComputePoolDeleting", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent workload admission")
	}

	if _, found, err := store.GetDesired(
		ctx, clusterID, "Workload", workloadID,
	); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("concurrent pool delete admitted orphan workload")
	}
}

func TestPostgresCreateWorkloadDesiredPreservesLostAckReplay(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-replay")
	poolID := domain.ID("pool-replay")
	workload := agent.DesiredResource{
		Kind: "Workload", ID: "train-replay", Generation: 1,
		Spec: map[string]any{"poolID": poolID, "image": "example/train:v1"},
	}

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind:       "ComputePool",
		ID:         poolID,
		Generation: 1,
		Spec: map[string]any{
			"acceleratorBindings": []domain.AcceleratorBinding{{
				Class: "h100", ResourceName: "nvidia.com/gpu", Flavor: "h100",
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorkloadDesired(
		ctx, clusterID, poolID, "h100", workload,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx, clusterID, "ComputePool", poolID, time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorkloadDesired(
		ctx, clusterID, poolID, "h100", workload,
	); err != nil {
		t.Fatalf("identical lost-ACK replay must succeed: %v", err)
	}
}

func TestPostgresFinalizeDesiredOwnedIsIdempotentAfterLostAck(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-finalize-replay")
	resourceID := domain.ID("train-finalize-replay")

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: 4, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx, clusterID, "Workload", resourceID, time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(
		ctx, clusterID, "Workload", resourceID, "agent-a", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%t err=%v", claimed, err)
	}

	if err := store.FinalizeDesiredOwned(
		ctx, clusterID, "Workload", resourceID, 4, "agent-a",
	); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesiredOwned(
		ctx, clusterID, "Workload", resourceID, 4, "agent-a",
	); err != nil {
		t.Fatalf("same-generation finalize replay must succeed: %v", err)
	}
	if err := store.FinalizeDesiredOwned(
		ctx, clusterID, "Workload", resourceID, 3, "agent-a",
	); !errors.Is(err, agentstore.ErrStaleGeneration) {
		t.Fatalf("older replay err=%v, want ErrStaleGeneration", err)
	}
	if err := store.FinalizeDesiredOwned(
		ctx, clusterID, "Workload", resourceID, 5, "agent-a",
	); !errors.Is(err, agentstore.ErrDesiredNotFound) {
		t.Fatalf("future replay err=%v, want ErrDesiredNotFound", err)
	}
}

func TestPostgresUpsertCannotCancelDeletionLifecycle(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-delete-preserved")
	resourceID := domain.ID("pool-delete-preserved")
	at := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "ComputePool", ID: resourceID, Generation: 3,
		Spec: map[string]any{"quota": 4},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx, clusterID, "ComputePool", resourceID, at,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "ComputePool", ID: resourceID, Generation: 4,
		Spec: map[string]any{"quota": 8},
	}); err != nil {
		t.Fatal(err)
	}

	got, found, err := store.GetDesired(
		ctx, clusterID, "ComputePool", resourceID,
	)
	if err != nil || !found {
		t.Fatalf("desired missing: found=%t err=%v", found, err)
	}
	if got.Generation != 4 || got.Spec["quota"] != float64(8) {
		t.Fatalf("desired update missing: %#v", got)
	}
	if got.DeletionTimestamp == nil || !got.DeletionTimestamp.Equal(at) {
		t.Fatalf("upsert cancelled deletion timestamp: %#v", got.DeletionTimestamp)
	}
	if len(got.Finalizers) != 1 ||
		got.Finalizers[0] != agentstore.ProviderCleanupFinalizer {
		t.Fatalf("upsert cancelled cleanup finalizer: %#v", got.Finalizers)
	}
}

func TestPostgresRecreateWaitsForFinalizationReceiptAndConsumesTombstone(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-finalize-recreate-serialized")
	resourceID := domain.ID("train-finalize-recreate-serialized")

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: 7,
		Spec: map[string]any{"image": "example/v7"},
	}); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := lockDesiredLifecycleTx(
		ctx, tx, clusterID, "Workload", resourceID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO deletion_tombstones (cluster_id, kind, resource_id, generation)
VALUES ($1, 'Workload', $2, 7)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET generation = 7
`, clusterID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM desired_resources
WHERE cluster_id = $1 AND kind = 'Workload' AND resource_id = $2
`, clusterID, resourceID); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
			Kind: "Workload", ID: resourceID, Generation: 8,
			Spec: map[string]any{"image": "example/v8"},
		})
	}()

	select {
	case err := <-result:
		t.Fatalf("recreate crossed uncommitted finalization boundary: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: lifecycle advisory lock fences the recreate.
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("recreate after finalization commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for serialized recreate")
	}

	if _, found, err := store.FinalizedGeneration(
		ctx, clusterID, "Workload", resourceID,
	); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("successful newer-generation recreate must consume old tombstone")
	}
	got, found, err := store.GetDesired(
		ctx, clusterID, "Workload", resourceID,
	)
	if err != nil || !found || got.Generation != 8 {
		t.Fatalf("unexpected recreated desired: found=%t err=%v desired=%#v", found, err, got)
	}
}

func TestPostgresOwnedFinalizePrefersCurrentDesiredOverOlderTombstone(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-legacy-coexist")
	resourceID := domain.ID("train-legacy-coexist")

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: 8,
		Spec: map[string]any{"image": "example/v8"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO deletion_tombstones (cluster_id, kind, resource_id, generation)
VALUES ($1, 'Workload', $2, 7)
ON CONFLICT (cluster_id, kind, resource_id) DO UPDATE SET generation = 7
`, clusterID, resourceID); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx, clusterID, "Workload", resourceID, time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(
		ctx, clusterID, "Workload", resourceID, "agent-new", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%t err=%v", claimed, err)
	}

	if err := store.FinalizeDesiredOwned(
		ctx, clusterID, "Workload", resourceID, 8, "agent-new",
	); err != nil {
		t.Fatalf("current desired must not be shadowed by older tombstone: %v", err)
	}
	finalized, found, err := store.FinalizedGeneration(
		ctx, clusterID, "Workload", resourceID,
	)
	if err != nil || !found || finalized != 8 {
		t.Fatalf("finalized generation=%d found=%t err=%v", finalized, found, err)
	}
}


func TestPostgresCreateWorkloadDesiredRejectsProviderMismatch(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-provider-mismatch")
	poolID := domain.ID("pool-provider-mismatch")

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "ComputePool",
		ID: poolID,
		Generation: 1,
		Spec: map[string]any{
			"provider": "kueue",
			"acceleratorBindings": []domain.AcceleratorBinding{{
				Class: "h100", ResourceName: "nvidia.com/gpu", Flavor: "h100",
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	err := store.CreateWorkloadDesired(
		ctx,
		clusterID,
		poolID,
		"h100",
		agent.DesiredResource{
			Kind: "Workload",
			ID: "train-provider-mismatch",
			Generation: 1,
			Spec: map[string]any{
				"provider": "volcano",
				"poolID": poolID,
			},
		},
	)
	if !errors.Is(err, agentstore.ErrProviderIdentityMismatch) {
		t.Fatalf("err=%v, want ErrProviderIdentityMismatch", err)
	}
}
