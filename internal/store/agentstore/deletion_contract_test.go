package agentstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func TestMissingDesiredReportsAreReconciledOnCreation(t *testing.T) {
	for _, reported := range []int64{6, 7, 8} {
		t.Run(string(rune('0'+reported)), func(t *testing.T) {
			store := NewMemory()
			ctx := context.Background()
			if err := store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: "train", ObservedGeneration: reported}}); err != nil {
				t.Fatalf("report before desired must remain supported: %v", err)
			}
			if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "train", Generation: 7}); err != nil {
				t.Fatal(err)
			}
			observation, found, err := store.GetObservation(ctx, "cluster-a", "Workload", "train")
			if err != nil || found != (reported == 7) {
				t.Fatalf("provisional observation found=%t err=%v, reported=%d", found, err, reported)
			}
			if found && observation.ObservedGeneration != 7 {
				t.Fatalf("unexpected retained observation: %#v", observation)
			}
			if err := store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: "train", ObservedGeneration: 6}}); err != nil {
				t.Fatalf("report after creation must use desired CAS: %v", err)
			}
			if got, found, _ := store.GetObservation(ctx, "cluster-a", "Workload", "train"); found && got.ObservedGeneration != 7 {
				t.Fatalf("stale report persisted after creation: %#v", got)
			}
		})
	}
}

func TestFinalizeRequiresEvidenceAndRetainsTombstone(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	for _, id := range []domain.ID{"sibling", "train"} {
		if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: id, Generation: 7}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MarkDesiredDeleting(ctx, "cluster-a", "Workload", "train", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, observation := range []agent.Observation{
		{Kind: "Workload", ID: "train", ObservedGeneration: 7},
		{Kind: "Workload", ID: "train", ObservedGeneration: 7, EvidenceRefs: []string{"provider://pending"}},
		{Kind: "Workload", ID: "train", ObservedGeneration: 7, Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "Deleted"}}},
	} {
		if err := store.Report(ctx, "cluster-a", []agent.Observation{observation}); err != nil {
			t.Fatal(err)
		}
		if err := store.FinalizeDesired(ctx, "cluster-a", "Workload", "train", 7); !errors.Is(err, ErrDeletionEvidenceRequired) {
			t.Fatalf("finalize without gone evidence: %v", err)
		}
		if items, _ := store.Desired(ctx, "cluster-a"); len(items) != 2 || items[0].ID != "sibling" || items[1].ID != "train" {
			t.Fatalf("failed finalization mutated desired resources: %#v", items)
		}
	}
	if err := store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind: "Workload", ID: "train", ObservedGeneration: 7,
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "Deleted"}}, EvidenceRefs: []string{"provider://gone/train"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesired(ctx, "cluster-a", "Workload", "train", 7); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := store.GetObservation(ctx, "cluster-a", "Workload", "train"); found {
		t.Fatal("runtime observation survived finalization")
	}
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "train", Generation: 8}); err != nil {
		t.Fatal(err)
	}
	tombstone, found, err := store.GetDeletionTombstone(ctx, "cluster-a", "Workload", "train")
	if err != nil || !found || tombstone.Generation != 7 || tombstone.FinalizedAt.IsZero() || len(tombstone.EvidenceRefs) != 1 || tombstone.EvidenceRefs[0] != "provider://gone/train" {
		t.Fatalf("final evidence was not retained: %#v found=%t err=%v", tombstone, found, err)
	}
	tombstone.EvidenceRefs[0] = "mutated"
	retained, _, _ := store.GetDeletionTombstone(ctx, "cluster-a", "Workload", "train")
	if retained.EvidenceRefs[0] != "provider://gone/train" {
		t.Fatal("caller mutated durable tombstone")
	}
}

func TestFinalizePreservesUnknownFinalizer(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "train", Generation: 1, Finalizers: []string{"external.example/hold"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(ctx, "cluster-a", "Workload", "train", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesired(ctx, "cluster-a", "Workload", "train", 1); !errors.Is(err, ErrFinalizersRemaining) {
		t.Fatalf("unknown finalizer must block hard delete: %v", err)
	}
}
