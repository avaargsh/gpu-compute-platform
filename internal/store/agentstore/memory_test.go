package agentstore

import (
	"context"
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
		Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "AwaitingPods", LastTransitionTime: t1}},
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
