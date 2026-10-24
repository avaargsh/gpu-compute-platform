package cluster

import (
	"context"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
	"k8s.io/client-go/rest"
)

type recordingAdapter struct {
	poolReconcileCalls     int
	workloadReconcileCalls int
	poolDeleteCalls        int
	workloadDeleteCalls    int
}

func (a *recordingAdapter) ReconcilePool(_ context.Context, in provider.PoolProjection) (provider.PoolObservation, error) {
	a.poolReconcileCalls++
	return provider.PoolObservation{ObservedGeneration: in.Generation}, nil
}

func (a *recordingAdapter) DeletePool(_ context.Context, _ provider.PoolProjection) (provider.DeletionObservation, error) {
	a.poolDeleteCalls++
	return provider.DeletionObservation{Gone: true}, nil
}

func (a *recordingAdapter) ReconcileWorkload(_ context.Context, in provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	a.workloadReconcileCalls++
	return provider.WorkloadObservation{ObservedGeneration: in.Generation}, nil
}

func (a *recordingAdapter) DeleteWorkload(_ context.Context, _ provider.WorkloadProjection) (provider.DeletionObservation, error) {
	a.workloadDeleteCalls++
	return provider.DeletionObservation{Gone: true}, nil
}

func TestRuntimeDispatchesFrozenProviderIdentity(t *testing.T) {
	kueueAdapter := &recordingAdapter{}
	otherAdapter := &recordingAdapter{}
	runtime, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue":     kueueAdapter,
		"test-only": otherAdapter,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.ReconcilePool(context.Background(), provider.PoolProjection{
		Provider:   "kueue",
		Generation: 7,
	}); err != nil {
		t.Fatal(err)
	}
	if kueueAdapter.poolReconcileCalls != 1 || otherAdapter.poolReconcileCalls != 0 {
		t.Fatalf(
			"provider dispatch crossed identity: kueue=%d other=%d",
			kueueAdapter.poolReconcileCalls,
			otherAdapter.poolReconcileCalls,
		)
	}

	if _, err := runtime.ReconcileWorkload(context.Background(), provider.WorkloadProjection{
		Provider:   "test-only",
		Generation: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if otherAdapter.workloadReconcileCalls != 1 || kueueAdapter.workloadReconcileCalls != 0 {
		t.Fatalf(
			"workload dispatch crossed identity: kueue=%d other=%d",
			kueueAdapter.workloadReconcileCalls,
			otherAdapter.workloadReconcileCalls,
		)
	}
}

func TestRuntimeProviderNamesAreDeterministicRegistryFacts(t *testing.T) {
	runtime, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"volcano-test-only": &recordingAdapter{},
		"kueue":             &recordingAdapter{},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := runtime.ProviderNames()
	if len(got) != 2 || got[0] != "kueue" || got[1] != "volcano-test-only" {
		t.Fatalf("provider names=%v, want deterministic sorted registry facts", got)
	}

	got[0] = "mutated"
	again := runtime.ProviderNames()
	if again[0] != "kueue" {
		t.Fatalf("provider names leaked mutable registry state: %v", again)
	}
}

func TestRuntimeFailsClosedForMissingOrUnknownProvider(t *testing.T) {
	runtime, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue": &recordingAdapter{},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"", "volcano"} {
		_, err := runtime.ReconcilePool(context.Background(), provider.PoolProjection{
			Provider:   name,
			Generation: 1,
		})
		if err == nil {
			t.Fatalf("provider %q unexpectedly dispatched", name)
		}
		if !strings.Contains(err.Error(), "provider") {
			t.Fatalf("provider %q error=%v", name, err)
		}
	}
}

func TestRuntimeRequiresExplicitNonNilAdapters(t *testing.T) {
	if _, err := NewRuntimeWithAdapters(nil); err == nil {
		t.Fatal("empty provider registry must fail closed")
	}
	if _, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"": &recordingAdapter{},
	}); err == nil {
		t.Fatal("empty provider name must fail closed")
	}
	if _, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue": nil,
	}); err == nil {
		t.Fatal("nil provider adapter must fail closed")
	}
}


func TestProductionRuntimeRegistersOnlyAcceptedKueueProvider(t *testing.T) {
	// NewRuntime is the production registration path. Keep this separate from
	// NewRuntimeWithAdapters tests, which intentionally permit test-only adapters.
	clients, err := NewClients(&rest.Config{Host: "https://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(clients)
	if err != nil {
		t.Fatal(err)
	}

	got := runtime.ProviderNames()
	if len(got) != 1 || got[0] != KueueProviderName {
		t.Fatalf("production provider registry=%v, want only %q until another provider is independently accepted", got, KueueProviderName)
	}
	if _, err := runtime.adapter("volcano"); err == nil {
		t.Fatal("unaccepted Volcano provider must not be dispatchable by the production runtime")
	}
}

func TestRuntimeFailsClosedAcrossAllLifecycleMethods(t *testing.T) {
	adapter := &recordingAdapter{}
	runtime, err := NewRuntimeWithAdapters(map[string]provider.Adapter{
		"kueue": adapter,
	})
	if err != nil {
		t.Fatal(err)
	}

	operations := []struct {
		name string
		call func(string) error
	}{
		{
			name: "ReconcilePool",
			call: func(name string) error {
				_, err := runtime.ReconcilePool(context.Background(), provider.PoolProjection{Provider: name, Generation: 1})
				return err
			},
		},
		{
			name: "ReconcileWorkload",
			call: func(name string) error {
				_, err := runtime.ReconcileWorkload(context.Background(), provider.WorkloadProjection{Provider: name, Generation: 1})
				return err
			},
		},
		{
			name: "DeletePool",
			call: func(name string) error {
				_, err := runtime.DeletePool(context.Background(), provider.PoolProjection{Provider: name, Generation: 1})
				return err
			},
		},
		{
			name: "DeleteWorkload",
			call: func(name string) error {
				_, err := runtime.DeleteWorkload(context.Background(), provider.WorkloadProjection{Provider: name, Generation: 1})
				return err
			},
		},
	}

	for _, providerName := range []string{"", "volcano"} {
		for _, operation := range operations {
			t.Run(operation.name+"/"+providerName, func(t *testing.T) {
				err := operation.call(providerName)
				if err == nil {
					t.Fatalf("provider %q unexpectedly dispatched for %s", providerName, operation.name)
				}
				if !strings.Contains(err.Error(), "provider") {
					t.Fatalf("provider %q returned an unclear dispatch error: %v", providerName, err)
				}
			})
		}
	}

	if adapter.poolReconcileCalls != 0 || adapter.workloadReconcileCalls != 0 ||
		adapter.poolDeleteCalls != 0 || adapter.workloadDeleteCalls != 0 {
		t.Fatalf("unknown provider reached the registered adapter: %#v", adapter)
	}
}
