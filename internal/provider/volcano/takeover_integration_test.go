package volcano_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/provider/volcano"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestTakeoverPreservesProviderEvidenceAndFencesStaleWriter(t *testing.T) {
	ctx := context.Background()
	store := agentstore.NewMemory()
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind: "Workload", ID: "train-volcano", Generation: 7, Spec: map[string]any{},
	}})

	claimed, err := store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-volcano", "agent-a", 1)
	if err != nil || !claimed { t.Fatalf("agent-a claim=%v err=%v", claimed, err) }

	state := volcano.WorkloadState{
		Phase: "Running", Admitted: true, PodsReady: true,
		JobRef: "volcano://cluster-a/namespaces/default/jobs/job-train-volcano",
		PodGroupRef: "volcano://cluster-a/namespaces/default/podgroups/pg-train-volcano",
		PodRefs: []string{"k8s://cluster-a/namespaces/default/pods/pod-1"},
	}
	providerObs, err := volcano.TranslateWorkloadObservation(7, state)
	if err != nil { t.Fatal(err) }
	report := func(owner string) error {
		return store.Report(ctx, "cluster-a", []agent.Observation{{
			Kind: "Workload", ID: "train-volcano", ObservedGeneration: providerObs.ObservedGeneration,
			LeaseOwner: owner, Conditions: providerObs.Conditions, EvidenceRefs: providerObs.EvidenceRefs,
		}})
	}
	if err := report("agent-a"); err != nil { t.Fatal(err) }

	// Simulate process death and lease expiry by waiting for the deliberately
	// short lease. The provider object/evidence identity remains unchanged.
	time.Sleep(1100 * time.Millisecond)
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-volcano", "agent-b", 30)
	if err != nil || !claimed { t.Fatalf("agent-b takeover=%v err=%v", claimed, err) }

	if err := report("agent-a"); !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale report err=%v, want ErrLeaseLost", err)
	}
	if err := report("agent-b"); err != nil { t.Fatalf("takeover report: %v", err) }

	got, found, err := store.GetObservation(ctx, "cluster-a", "Workload", "train-volcano")
	if err != nil || !found { t.Fatalf("observation found=%v err=%v", found, err) }
	if got.LeaseOwner != "agent-b" { t.Fatalf("owner=%q, want agent-b", got.LeaseOwner) }
	if len(got.EvidenceRefs) != len(providerObs.EvidenceRefs) {
		t.Fatalf("provider evidence identity changed across takeover: %v vs %v", got.EvidenceRefs, providerObs.EvidenceRefs)
	}
	for i := range got.EvidenceRefs {
		if got.EvidenceRefs[i] != providerObs.EvidenceRefs[i] {
			t.Fatalf("provider evidence[%d]=%q, want %q", i, got.EvidenceRefs[i], providerObs.EvidenceRefs[i])
		}
	}

	if err := store.MarkDesiredDeleting(ctx, "cluster-a", "Workload", "train-volcano", now); err != nil { t.Fatal(err) }
	if err := store.FinalizeDesiredOwned(ctx, "cluster-a", "Workload", "train-volcano", 7, "agent-a"); !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale finalize err=%v, want ErrLeaseLost", err)
	}
	if err := store.FinalizeDesiredOwned(ctx, "cluster-a", "Workload", "train-volcano", 7, "agent-b"); err != nil {
		t.Fatalf("takeover finalize: %v", err)
	}
}
