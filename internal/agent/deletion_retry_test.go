package agent

import (
	"context"
	"testing"
	"time"
)

func TestDeletionWaitsForGoneAndClearsRetryAfterFinalize(t *testing.T) {
	now := time.Date(2026, 9, 30, 7, 0, 0, 0, time.UTC)
	control := &fakeControlPlane{desired: []DesiredResource{{
		Kind: "Workload", ID: "train", Generation: 7, DeletionTimestamp: &now,
		Spec: map[string]any{"poolID": "pool", "namespace": "project", "image": "train", "accelerator": map[string]any{"class": "h100", "quota": float64(1)}},
	}}}
	runtime := &fakeRuntime{deletionPending: true}
	runner := NewRunner("cluster-a", control, runtime)
	runner.now = func() time.Time { return now }
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.desired) != 1 || len(control.reported) != 0 || len(runner.retries) != 1 {
		t.Fatalf("pending cleanup finalized or reported gone: desired=%#v observations=%#v retries=%#v", control.desired, control.reported, runner.retries)
	}
	runtime.deletionPending = false
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.workloadDeleteCalls != 1 {
		t.Fatal("backoff failed to suppress an immediate retry")
	}
	now = now.Add(time.Minute)
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.desired) != 0 || len(control.reported) != 1 || len(runner.retries) != 0 || control.reported[0].Conditions[0].Reason != "Deleted" {
		t.Fatalf("gone cleanup did not finalize and clear retries: desired=%#v observations=%#v retries=%#v", control.desired, control.reported, runner.retries)
	}
}

func TestMissingDesiredClearsLocalRetry(t *testing.T) {
	runner := NewRunner("cluster-a", &fakeControlPlane{}, &fakeRuntime{})
	runner.scheduleRetry("Workload/gone", 7)
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.retries) != 0 {
		t.Fatalf("retry survived missing desired: %#v", runner.retries)
	}
}
