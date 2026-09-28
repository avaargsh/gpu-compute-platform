package agent

import (
	"context"
	"testing"
	"time"
)

type lifecycleControl struct {
	fakeControlPlane
	registered int
	heartbeats int
}

func (f *lifecycleControl) Register(context.Context, Registration) error {
	f.registered++
	return nil
}

func (f *lifecycleControl) Heartbeat(context.Context, Heartbeat) error {
	f.heartbeats++
	return nil
}

func TestLifecycleRegistersAndTicksImmediately(t *testing.T) {
	control := &lifecycleControl{}
	runner := NewRunner("cluster-a", control, &fakeRuntime{})
	lifecycle := NewLifecycle("cluster-a", control, runner, "dev", "v1.34.0", time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := lifecycle.Run(ctx)
	if err == nil {
		t.Fatal("expected canceled context")
	}
	if control.registered != 1 {
		t.Fatalf("registered=%d, want 1", control.registered)
	}
	if control.heartbeats != 1 {
		t.Fatalf("heartbeats=%d, want 1", control.heartbeats)
	}
}
