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

func TestPostgresStateSurvivesStoreReconstruction(t *testing.T) {
	db := openContractDB(t)
	ctx := context.Background()
	clusterID := domain.ID("cluster-restart")
	resourceID := domain.ID("train-restart")
	const generation int64 = 9

	first := New(db)
	if err := first.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind:       "Workload",
		ID:         resourceID,
		Generation: generation,
		Spec: map[string]any{
			"projectID": "project-1",
			"poolID":    "pool-1",
			"image":     "example/train:v9",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.Report(ctx, clusterID, []agent.Observation{{
		Kind:               "Workload",
		ID:                 resourceID,
		ObservedGeneration: generation,
		Conditions:         []domain.Condition{{Type: "Ready", Status: "True", Reason: "PodsReady"}},
		EvidenceRefs:       []string{"k8s://cluster-restart/namespaces/project-1/jobs/job-train-restart"},
	}}); err != nil {
		t.Fatal(err)
	}
	claimed, err := first.ClaimReconcileLease(ctx, clusterID, "Workload", resourceID, "agent-before-restart", 120)
	if err != nil || !claimed {
		t.Fatalf("initial lease claim: claimed=%t err=%v", claimed, err)
	}

	restarted := New(db)

	desired, found, err := restarted.GetDesired(ctx, clusterID, "Workload", resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || desired.Generation != generation || desired.Spec["image"] != "example/train:v9" {
		t.Fatalf("desired state did not survive store reconstruction: %#v found=%t", desired, found)
	}

	observation, found, err := restarted.GetObservation(ctx, clusterID, "Workload", resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || observation.ObservedGeneration != generation || len(observation.EvidenceRefs) != 1 {
		t.Fatalf("observation did not survive store reconstruction: %#v found=%t", observation, found)
	}

	claimed, err = restarted.ClaimReconcileLease(ctx, clusterID, "Workload", resourceID, "agent-after-restart", 120)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("store reconstruction must not erase an unexpired reconcile lease")
	}
}

func TestPostgresRestartPreservesDeletionIntentAndTombstoneFence(t *testing.T) {
	db := openContractDB(t)
	ctx := context.Background()
	clusterID := domain.ID("cluster-restart-delete")
	resourceID := domain.ID("train-restart-delete")
	const generation int64 = 12

	first := New(db)
	if err := first.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation,
		Spec: map[string]any{"image": "example/train:v12"},
	}); err != nil {
		t.Fatal(err)
	}
	deletingAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := first.MarkDesiredDeleting(
		ctx, clusterID, "Workload", resourceID, deletingAt,
	); err != nil {
		t.Fatal(err)
	}

	restarted := New(db)
	deleting, found, err := restarted.GetDesired(
		ctx, clusterID, "Workload", resourceID,
	)
	if err != nil || !found {
		t.Fatalf("deleting desired missing after restart: found=%t err=%v", found, err)
	}
	if deleting.DeletionTimestamp == nil ||
		!deleting.DeletionTimestamp.Equal(deletingAt) {
		t.Fatalf("deletion intent lost after restart: %#v", deleting.DeletionTimestamp)
	}
	if len(deleting.Finalizers) != 1 ||
		deleting.Finalizers[0] != agentstore.ProviderCleanupFinalizer {
		t.Fatalf("cleanup finalizer lost after restart: %#v", deleting.Finalizers)
	}

	if err := restarted.FinalizeDesired(
		ctx, clusterID, "Workload", resourceID, generation,
	); err != nil {
		t.Fatal(err)
	}

	restartedAgain := New(db)
	finalizedGeneration, found, err := restartedAgain.FinalizedGeneration(
		ctx, clusterID, "Workload", resourceID,
	)
	if err != nil || !found || finalizedGeneration != generation {
		t.Fatalf(
			"tombstone lost after restart: generation=%d found=%t err=%v",
			finalizedGeneration,
			found,
			err,
		)
	}

	if err := restartedAgain.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation,
		Spec: map[string]any{"image": "example/train:v12"},
	}); !errors.Is(err, agentstore.ErrStaleGeneration) {
		t.Fatalf("restart weakened tombstone fence: err=%v", err)
	}

	if err := restartedAgain.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: resourceID, Generation: generation + 1,
		Spec: map[string]any{"image": "example/train:v13"},
	}); err != nil {
		t.Fatalf("new generation recreate after restart: %v", err)
	}
	if _, found, err := restartedAgain.FinalizedGeneration(
		ctx, clusterID, "Workload", resourceID,
	); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("successful newer recreate must consume restarted tombstone")
	}
}

