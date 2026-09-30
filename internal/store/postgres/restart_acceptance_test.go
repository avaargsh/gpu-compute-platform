package postgres

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
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
