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
	store.SetDesired("cluster-a", []agent.DesiredResource{
		{Kind: "Workload", ID: "train-1", Generation: 8},
		{Kind: "ComputePool", ID: "pool-h100", Generation: 3},
		{Kind: "Workload", ID: "train-2", Generation: 1},
	})

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
	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 8,
	}})

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
	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 1,
	}})

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

func TestMemoryUpsertCannotCancelDeletionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	store.SetDesired(clusterID, []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 3, Spec: map[string]any{"image": "old"},
	}})
	at := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", "train-1", at); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: "train-1", Generation: 4, Spec: map[string]any{"image": "new"},
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetDesired(ctx, clusterID, "Workload", "train-1")
	if err != nil || !ok {
		t.Fatalf("desired missing: ok=%t err=%v", ok, err)
	}
	if got.DeletionTimestamp == nil || !got.DeletionTimestamp.Equal(at) {
		t.Fatalf("upsert must preserve deletion timestamp: %#v", got.DeletionTimestamp)
	}
	if !containsFinalizer(got.Finalizers, ProviderCleanupFinalizer) {
		t.Fatalf("upsert must preserve cleanup finalizer: %#v", got.Finalizers)
	}
}

func TestFinalizeCreatesGenerationTombstoneAndCleansRuntimeState(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	store.SetDesired(clusterID, []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 4, Spec: map[string]any{"image": "example/train:latest"},
	}})
	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 4,
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "Deleted"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(ctx, clusterID, "Workload", "train-1", time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(ctx, clusterID, "Workload", "train-1", "worker-a", 120)
	if err != nil || !claimed {
		t.Fatalf("claim before finalize: claimed=%t err=%v", claimed, err)
	}

	if err := store.FinalizeDesired(ctx, clusterID, "Workload", "train-1", 4); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.GetObservation(ctx, clusterID, "Workload", "train-1"); err != nil || ok {
		t.Fatalf("finalize must clean observation: ok=%t err=%v", ok, err)
	}
	key := string(clusterID) + "/Workload/train-1"
	if _, ok := store.leases[key]; ok {
		t.Fatal("finalize must clean reconcile lease")
	}
	claimed, err = store.ClaimReconcileLease(ctx, clusterID, "Workload", "train-1", "worker-b", 120)
	if err != nil || claimed {
		t.Fatalf("missing desired state must not allow a new lease: claimed=%t err=%v", claimed, err)
	}

	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 4,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", Reason: "LateOldAgent"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.GetObservation(ctx, clusterID, "Workload", "train-1"); ok {
		t.Fatal("tombstone must suppress late observation at finalized generation")
	}

	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 5,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", Reason: "FutureReport"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.GetObservation(ctx, clusterID, "Workload", "train-1"); ok {
		t.Fatal("observation without current desired state must be ignored")
	}

	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: "train-1", Generation: 4, Spec: map[string]any{},
	}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("recreate at tombstoned generation err=%v, want stale generation", err)
	}
	if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
		Kind: "Workload", ID: "train-1", Generation: 5, Spec: map[string]any{},
	}); err != nil {
		t.Fatalf("newer generation must be allowed to recreate resource: %v", err)
	}
	if _, ok, _ := store.GetObservation(ctx, clusterID, "Workload", "train-1"); ok {
		t.Fatal("recreated desired state must not inherit a pre-intent observation")
	}
	if err := store.Report(ctx, clusterID, []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 5,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True", Reason: "CurrentReport"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := store.GetObservation(ctx, clusterID, "Workload", "train-1"); !ok || got.ObservedGeneration != 5 {
		t.Fatalf("current observation after recreate must persist: %#v", got)
	}
}

func TestMemoryLeaseTakeoverFencesStaleWrites(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind:       "Workload",
		ID:         "train-fenced",
		Generation: 7,
		Spec:       map[string]any{"image": "example/train:v7"},
	}})

	claimed, err := store.ClaimReconcileLease(
		ctx, "cluster-a", "Workload", "train-fenced", "agent-a", 10,
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
		t.Fatal(err)
	}

	now = now.Add(11 * time.Second)
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
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale report err=%v, want ErrLeaseLost", err)
	}

	if err := store.Report(ctx, "cluster-a", []agent.Observation{{
		Kind:               "Workload",
		ID:                 "train-fenced",
		ObservedGeneration: 7,
		LeaseOwner:         "agent-b",
		EvidenceRefs:       []string{"evidence://agent-b"},
	}}); err != nil {
		t.Fatal(err)
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
		ctx, "cluster-a", "Workload", "train-fenced", now,
	); err != nil {
		t.Fatal(err)
	}

	err = store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-fenced", 7, "agent-a",
	)
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale finalize err=%v, want ErrLeaseLost", err)
	}

	if err := store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-fenced", 7, "agent-b",
	); err != nil {
		t.Fatalf("takeover owner finalize: %v", err)
	}
}

func TestMemoryUpsertSameGenerationIsIdempotentButImmutable(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	clusterID := domain.ID("cluster-a")
	original := agent.DesiredResource{
		Kind:       "ComputePool",
		ID:         "pool-1",
		Generation: 7,
		Spec: map[string]any{
			"namespace": "project-1",
			"quota":     float64(8),
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
		"quota":     float64(16),
	}
	if err := store.UpsertDesired(ctx, clusterID, mutated); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("same-generation mutation err=%v, want ErrStaleGeneration", err)
	}

	got, found, err := store.GetDesired(ctx, clusterID, "ComputePool", "pool-1")
	if err != nil || !found {
		t.Fatalf("desired missing: found=%t err=%v", found, err)
	}
	if got.Spec["quota"] != float64(8) {
		t.Fatalf("same-generation mutation changed desired spec: %#v", got.Spec)
	}
}

func TestMemoryLeaseCannotPreclaimMissingDesiredIdentity(t *testing.T) {
	store := NewMemory()
	claimed, err := store.ClaimReconcileLease(
		context.Background(),
		"cluster-a",
		"Workload",
		"future-workload",
		"worker-a",
		120,
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("lease must not be created before desired state exists")
	}
	if len(store.leases) != 0 {
		t.Fatalf("orphan lease persisted: %#v", store.leases)
	}
}

func TestMemoryCreateWorkloadDesiredPreservesReplayAfterPoolDeleting(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	pool := agent.DesiredResource{
		Kind:       "ComputePool",
		ID:         "pool-1",
		Generation: 1,
		Spec: map[string]any{
			"acceleratorBindings": []domain.AcceleratorBinding{{
				Class:        "h100",
				ResourceName: "nvidia.com/gpu",
				Flavor:       "h100",
			}},
		},
	}
	if err := store.UpsertDesired(ctx, "cluster-a", pool); err != nil {
		t.Fatal(err)
	}
	workload := agent.DesiredResource{
		Kind:       "Workload",
		ID:         "train-1",
		Generation: 1,
		Spec: map[string]any{
			"poolID": "pool-1",
			"accelerator": domain.AcceleratorRequest{
				Class: "h100",
				Quota: 1,
			},
		},
	}
	if err := store.CreateWorkloadDesired(
		ctx, "cluster-a", "pool-1", "h100", workload,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx, "cluster-a", "ComputePool", "pool-1", time.Now(),
	); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateWorkloadDesired(
		ctx, "cluster-a", "pool-1", "h100", workload,
	); err != nil {
		t.Fatalf("identical replay after pool deletion started must succeed: %v", err)
	}

	other := workload
	other.ID = "train-2"
	if err := store.CreateWorkloadDesired(
		ctx, "cluster-a", "pool-1", "h100", other,
	); !errors.Is(err, ErrComputePoolDeleting) {
		t.Fatalf("new workload under deleting pool err=%v, want ErrComputePoolDeleting", err)
	}
}

func TestMemoryCreateWorkloadDesiredRejectsCrossClusterIdentityRace(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	for _, clusterID := range []domain.ID{"cluster-a", "cluster-b"} {
		if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
			Kind:       "ComputePool",
			ID:         "pool-1",
			Generation: 1,
			Spec: map[string]any{
				"acceleratorBindings": []domain.AcceleratorBinding{{
					Class: "h100", ResourceName: "nvidia.com/gpu", Flavor: "h100",
				}},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	workload := agent.DesiredResource{
		Kind: "Workload", ID: "global-train", Generation: 1,
		Spec: map[string]any{"poolID": "pool-1"},
	}
	if err := store.CreateWorkloadDesired(
		ctx, "cluster-a", "pool-1", "h100", workload,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorkloadDesired(
		ctx, "cluster-b", "pool-1", "h100", workload,
	); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("cross-cluster duplicate err=%v, want ErrIdentityConflict", err)
	}
}

func TestMemoryFinalizeDesiredOwnedIsIdempotentAfterLostAck(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind: "Workload", ID: "train-finalize-replay", Generation: 4,
	}})
	if err := store.MarkDesiredDeleting(
		ctx, "cluster-a", "Workload", "train-finalize-replay", time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(
		ctx, "cluster-a", "Workload", "train-finalize-replay", "agent-a", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%t err=%v", claimed, err)
	}

	if err := store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-finalize-replay", 4, "agent-a",
	); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-finalize-replay", 4, "agent-a",
	); err != nil {
		t.Fatalf("same-generation finalize replay must succeed: %v", err)
	}
	if err := store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-finalize-replay", 3, "agent-a",
	); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("older replay err=%v, want ErrStaleGeneration", err)
	}
	if err := store.FinalizeDesiredOwned(
		ctx, "cluster-a", "Workload", "train-finalize-replay", 5, "agent-a",
	); !errors.Is(err, ErrDesiredNotFound) {
		t.Fatalf("future replay err=%v, want ErrDesiredNotFound", err)
	}
}

func TestMemoryUpsertRejectsHigherGenerationWhileDeleting(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-deleting", Generation: 3,
		Spec: map[string]any{"quota": float64(4)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(
		ctx, "cluster-a", "ComputePool", "pool-deleting", time.Now(),
	); err != nil {
		t.Fatal(err)
	}

	err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-deleting", Generation: 4,
		Spec: map[string]any{"quota": float64(8)},
	})
	if !errors.Is(err, ErrDesiredDeleting) {
		t.Fatalf("update during deletion err=%v, want ErrDesiredDeleting", err)
	}

	got, found, err := store.GetDesired(
		ctx, "cluster-a", "ComputePool", "pool-deleting",
	)
	if err != nil || !found {
		t.Fatalf("desired missing: found=%t err=%v", found, err)
	}
	if got.Generation != 3 || got.Spec["quota"] != float64(4) {
		t.Fatalf("deleting desired state mutated: %#v", got)
	}
}
