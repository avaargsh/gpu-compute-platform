package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type lifecycleControl struct {
	fakeControlPlane
	registered       int
	lastRegistration Registration
	heartbeats       int
	heartbeatErr     error
}

func (f *lifecycleControl) Register(_ context.Context, in Registration) error {
	f.registered++
	f.lastRegistration = in
	return nil
}

func (f *lifecycleControl) Heartbeat(context.Context, Heartbeat) error {
	f.heartbeats++
	err := f.heartbeatErr
	f.heartbeatErr = nil
	return err
}

func TestLifecycleRegistersAndTicksImmediately(t *testing.T) {
	control := &lifecycleControl{}
	runner := NewRunner("cluster-a", control, &fakeRuntime{})
	lifecycle := NewLifecycle("cluster-a", control, runner, "dev", "v1.34.0", domain.ClusterCapabilities{}, time.Hour)

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

func TestLifecycleSurvivesTransientTickFailure(t *testing.T) {
	control := &lifecycleControl{heartbeatErr: errors.New("temporary control-plane outage")}
	runner := NewRunner("cluster-a", control, &fakeRuntime{})
	lifecycle := NewLifecycle("cluster-a", control, runner, "dev", "v1.34.0", domain.ClusterCapabilities{}, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- lifecycle.Run(ctx) }()

	deadline := time.After(250 * time.Millisecond)
	for control.heartbeats < 2 {
		select {
		case err := <-done:
			t.Fatalf("lifecycle exited on transient tick failure: %v", err)
		case <-deadline:
			t.Fatal("lifecycle did not retry after transient tick failure")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run err=%v, want context canceled", err)
	}
}


func TestLifecycleRegistersDiscoveredCapabilities(t *testing.T) {
	control := &lifecycleControl{}
	runner := NewRunner("cluster-a", control, &fakeRuntime{})
	capabilities := domain.ClusterCapabilities{
		Kueue:        true,
		Accelerators: []string{"h100-80g", "metax-c500"},
	}
	lifecycle := NewLifecycle(
		"cluster-a",
		control,
		runner,
		"dev",
		"v1.34.0",
		capabilities,
		time.Hour,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = lifecycle.Run(ctx)

	got := control.lastRegistration.Capabilities
	if !got.Kueue || len(got.Accelerators) != 2 ||
		got.Accelerators[0] != "h100-80g" ||
		got.Accelerators[1] != "metax-c500" {
		t.Fatalf("registered capabilities=%#v", got)
	}
}
