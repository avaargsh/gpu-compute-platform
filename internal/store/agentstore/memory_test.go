package agentstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func TestMemoryReportStateMachine(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	t0 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)

	if err := store.Report(ctx, "cluster-a", []agent.Observation{
		{
			Kind: "Workload", ID: "train-1", ObservedGeneration: 8,
			Conditions:   []domain.Condition{{Type: "Ready", Status: "False", Reason: "Pending", LastTransitionTime: t0}},
			EvidenceRefs: []string{"evidence-v8-a"},
		},
		{
			Kind: "ComputePool", ID: "pool-h100", ObservedGeneration: 3,
			Conditions: []domain.Condition{{Type: "Ready", Status: "True", LastTransitionTime: t0}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 8,
		Conditions:   []domain.Condition{{Type: "Ready", Status: "False", Reason: "AwaitingPods", LastTransitionTime: t1}},
		EvidenceRefs: []string{"evidence-v8-b"},
	}}); err != nil {
		t.Fatal(err)
	}

	got := observationsByKey(store.Observations("cluster-a"))
	workload := got["Workload/train-1"]
	if workload.Conditions[0].Reason != "AwaitingPods" || !workload.Conditions[0].LastTransitionTime.Equal(t0) {
		t.Fatalf("same-generation runtime update must preserve transition time: %#v", workload)
	}
	if workload.EvidenceRefs[0] != "evidence-v8-b" {
		t.Fatalf("same-generation evidence must update: %#v", workload.EvidenceRefs)
	}
	if _, ok := got["ComputePool/pool-h100"]; !ok {
		t.Fatal("partial report must not delete an existing observation")
	}

	if err := store.Report(ctx, "cluster-a", []agent.Observation{
		{
			Kind: "Workload", ID: "train-1", ObservedGeneration: 7,
			Conditions:   []domain.Condition{{Type: "Ready", Status: "True", LastTransitionTime: t2}},
			EvidenceRefs: []string{"stale"},
		},
		{
			Kind: "Workload", ID: "train-2", ObservedGeneration: 1,
			Conditions:   []domain.Condition{{Type: "Ready", Status: "True", LastTransitionTime: t2}},
			EvidenceRefs: []string{"fresh"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	got = observationsByKey(store.Observations("cluster-a"))
	workload = got["Workload/train-1"]
	if workload.ObservedGeneration != 8 || workload.Conditions[0].Status != "False" || workload.EvidenceRefs[0] != "evidence-v8-b" {
		t.Fatalf("stale observation rolled state back: %#v", workload)
	}
	if fresh, ok := got["Workload/train-2"]; !ok || fresh.ObservedGeneration != 1 {
		t.Fatalf("stale sibling must not block fresh observation: %#v", got)
	}
}

func TestMemoryReportUsesNewTransitionTimeWhenStatusChanges(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	t0 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	_ = store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 8,
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", LastTransitionTime: t0}},
	}})
	_ = store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 8,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", LastTransitionTime: t1}},
	}})

	got := observationsByKey(store.Observations("cluster-a"))["Workload/train-1"]
	if got.Conditions[0].Status != "True" || !got.Conditions[0].LastTransitionTime.Equal(t1) {
		t.Fatalf("status transition must use current transition time: %#v", got)
	}
}

func observationsByKey(items []agent.Observation) map[string]agent.Observation {
	out := make(map[string]agent.Observation, len(items))
	for _, item := range items {
		out[item.Kind+"/"+string(item.ID)] = item
	}
	return out
}

func TestReportRejectsObservationStaleAgainstDesiredGeneration(t *testing.T) {
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	store.SetDesired(clusterID, []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 2,
	}})

	if err := store.Report(context.Background(), clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 1,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.GetObservation(context.Background(), clusterID, "Workload", "train-1"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("stale observation must not be persisted")
	}

	if err := store.Report(context.Background(), clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 2,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetObservation(context.Background(), clusterID, "Workload", "train-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.ObservedGeneration != 2 {
		t.Fatalf("current observation not persisted: %#v", got)
	}
}

func TestReportPreservesTransitionTimeAtCurrentGeneration(t *testing.T) {
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	t0 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	store.SetDesired(clusterID, []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 2,
	}})
	if err := store.Report(context.Background(), clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 2,
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "Pending", LastTransitionTime: t0}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Report(context.Background(), clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 2,
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "AwaitingPods", LastTransitionTime: t1}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetObservation(context.Background(), clusterID, "Workload", "train-1")
	if err != nil || !ok {
		t.Fatalf("observation missing: ok=%v err=%v", ok, err)
	}
	if len(got.Conditions) != 1 || got.Conditions[0].Reason != "AwaitingPods" || !got.Conditions[0].LastTransitionTime.Equal(t0) {
		t.Fatalf("same-status report must preserve transition time: %#v", got.Conditions)
	}
}

func TestMemoryReconcileLeaseLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	claimed, err := store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 30)
	if err != nil || !claimed {
		t.Fatalf("initial claim: claimed=%t err=%v", claimed, err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || claimed {
		t.Fatalf("competing claim must fail: claimed=%t err=%v", claimed, err)
	}

	now = now.Add(10 * time.Second)
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 30)
	if err != nil || !claimed {
		t.Fatalf("owner renew: claimed=%t err=%v", claimed, err)
	}

	now = now.Add(31 * time.Second)
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b", 30)
	if err != nil || !claimed {
		t.Fatalf("expired lease takeover: claimed=%t err=%v", claimed, err)
	}

	if err := store.ReleaseReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a"); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 30)
	if err != nil || claimed {
		t.Fatalf("non-owner release must not clear lease: claimed=%t err=%v", claimed, err)
	}

	if err := store.ReleaseReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-b"); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "train-1", "worker-a", 30)
	if err != nil || !claimed {
		t.Fatalf("owner release must clear lease: claimed=%t err=%v", claimed, err)
	}
}

func TestMemoryReconcileLeaseValidatesIdentityAndTTL(t *testing.T) {
	store := NewMemory()
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

func TestMemoryDesiredDeletionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	store.SetDesired(clusterID, []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 4, Spec: map[string]any{"image": "example/train:latest"},
	}})
	at := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)

	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", "train-1", at); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetDesired(ctx, clusterID, "Workload", "train-1")
	if err != nil || !ok {
		t.Fatalf("desired missing after mark-delete: ok=%t err=%v", ok, err)
	}
	if got.DeletionTimestamp == nil || !got.DeletionTimestamp.Equal(at) {
		t.Fatalf("unexpected deletion timestamp: %#v", got.DeletionTimestamp)
	}
	if len(got.Finalizers) != 1 || got.Finalizers[0] != ProviderCleanupFinalizer {
		t.Fatalf("unexpected finalizers: %#v", got.Finalizers)
	}

	later := at.Add(time.Minute)
	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", "train-1", later); err != nil {
		t.Fatal(err)
	}
	got, _, _ = store.GetDesired(ctx, clusterID, "Workload", "train-1")
	if got.DeletionTimestamp == nil || !got.DeletionTimestamp.Equal(at) || len(got.Finalizers) != 1 {
		t.Fatalf("mark-delete must be idempotent: %#v", got)
	}

	if err := store.FinalizeDesired(ctx, clusterID, "Workload", "train-1", 4); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.GetDesired(ctx, clusterID, "Workload", "train-1"); err != nil || ok {
		t.Fatalf("desired must be gone after finalize: ok=%t err=%v", ok, err)
	}
	if err := store.FinalizeDesired(ctx, clusterID, "Workload", "train-1", 4); !errors.Is(err, ErrDesiredNotFound) {
		t.Fatalf("second finalize err=%v, want ErrDesiredNotFound", err)
	}
}

func TestFinalizeDesiredRejectsActiveResource(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	store.SetDesired(clusterID, []agent.DesiredResource{{
		Kind: "Workload", ID: "train-active", Generation: 7, Spec: map[string]any{"image": "example/train:latest"},
	}})

	err := store.FinalizeDesired(ctx, clusterID, "Workload", "train-active", 7)
	if !errors.Is(err, ErrDesiredNotDeleting) {
		t.Fatalf("finalize active desired err=%v, want ErrDesiredNotDeleting", err)
	}
	if _, ok, getErr := store.GetDesired(ctx, clusterID, "Workload", "train-active"); getErr != nil || !ok {
		t.Fatalf("active desired must survive invalid finalize: ok=%t err=%v", ok, getErr)
	}
}
