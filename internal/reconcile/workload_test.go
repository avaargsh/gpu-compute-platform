package reconcile

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type fakeWorkloadProvider struct {
	last  provider.WorkloadProjection
	calls int
}

func (f *fakeWorkloadProvider) DeleteWorkload(_ context.Context, _ provider.WorkloadProjection) (provider.DeletionObservation, error) {
	return provider.DeletionObservation{Gone: true}, nil
}

func (f *fakeWorkloadProvider) ReconcileWorkload(_ context.Context, p provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	f.calls++
	f.last = p
	return provider.WorkloadObservation{ObservedGeneration: p.Generation}, nil
}

func TestWorkloadReconcilerResolvesPoolAcceleratorBinding(t *testing.T) {
	fp := &fakeWorkloadProvider{}
	r := NewWorkloadReconciler(fp)
	workload := domain.Workload{
		Metadata:  domain.Metadata{ID: "train-1", Generation: 3},
		ProjectID: "project-1",
		PoolID:    "pool-1",
		Spec: domain.WorkloadSpec{
			Image:       "example/train:latest",
			Accelerator: domain.AcceleratorRequest{Class: "h100-80g", Quota: 2},
		},
	}
	pool := domain.ComputePool{
		Metadata:  domain.Metadata{ID: "pool-1", Generation: 2},
		ProjectID: "project-1",
		Status:    domain.ResourceStatus{ObservedGeneration: 2, Conditions: []domain.Condition{{Type: "Ready", Status: "True"}}},
		Spec: domain.ComputePoolSpec{AcceleratorBindings: []domain.AcceleratorBinding{{
			Class: "h100-80g", ResourceName: "vendor.example/gpu", Flavor: "h100",
		}}},
	}
	_, err := r.Reconcile(context.Background(), workload, pool,
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a", Provider: "kueue"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if fp.last.AcceleratorBinding.ResourceName != "vendor.example/gpu" || fp.last.AcceleratorBinding.Flavor != "h100" {
		t.Fatalf("binding was not resolved: %#v", fp.last.AcceleratorBinding)
	}
}

func TestWorkloadReconcilerFailsClosedWhenClassIsUnbound(t *testing.T) {
	r := NewWorkloadReconciler(&fakeWorkloadProvider{})
	_, err := r.Reconcile(context.Background(),
		domain.Workload{
			Metadata:  domain.Metadata{ID: "train-1"},
			ProjectID: "project-1",
			PoolID:    "pool-1",
			Spec:      domain.WorkloadSpec{Image: "example/train:latest", Accelerator: domain.AcceleratorRequest{Class: "h100-80g", Quota: 1}},
		},
		domain.ComputePool{Metadata: domain.Metadata{ID: "pool-1"}, ProjectID: "project-1"},
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a"},
	)
	if err == nil {
		t.Fatal("expected unbound accelerator class to fail")
	}
}

func TestWorkloadReconcilerDoesNotCallProviderWhenPoolIsNotReady(t *testing.T) {
	fp := &fakeWorkloadProvider{}
	r := NewWorkloadReconciler(fp)
	_, err := r.Reconcile(context.Background(),
		domain.Workload{Metadata: domain.Metadata{ID: "train-1"}, ProjectID: "project-1", PoolID: "pool-1"},
		domain.ComputePool{Metadata: domain.Metadata{ID: "pool-1", Generation: 2}, ProjectID: "project-1",
			Status: domain.ResourceStatus{ObservedGeneration: 2, Conditions: []domain.Condition{{Type: "Ready", Status: "False"}}}},
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a"},
	)
	if err == nil {
		t.Fatal("expected not-ready compute pool to block workload")
	}
	if fp.calls != 0 {
		t.Fatalf("provider called %d times for not-ready pool", fp.calls)
	}
}

func TestWorkloadReconcilerDoesNotCallProviderWhenPoolObservationIsStale(t *testing.T) {
	fp := &fakeWorkloadProvider{}
	r := NewWorkloadReconciler(fp)
	_, err := r.Reconcile(context.Background(),
		domain.Workload{Metadata: domain.Metadata{ID: "train-1"}, ProjectID: "project-1", PoolID: "pool-1"},
		domain.ComputePool{Metadata: domain.Metadata{ID: "pool-1", Generation: 3}, ProjectID: "project-1",
			Status: domain.ResourceStatus{ObservedGeneration: 2, Conditions: []domain.Condition{{Type: "Ready", Status: "True"}}}},
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a"},
	)
	if err == nil {
		t.Fatal("expected stale compute pool observation to block workload")
	}
	if fp.calls != 0 {
		t.Fatalf("provider called %d times for stale pool observation", fp.calls)
	}
}

func TestWorkloadReconcilerLeavesProviderSpecificBindingValidationToProvider(t *testing.T) {
	fp := &fakeWorkloadProvider{}
	r := NewWorkloadReconciler(fp)
	workload := domain.Workload{
		Metadata:  domain.Metadata{ID: "train-dra", Generation: 3},
		ProjectID: "project-1",
		PoolID:    "pool-1",
		Spec: domain.WorkloadSpec{
			Image:       "example/train:latest",
			Accelerator: domain.AcceleratorRequest{Class: "h100-dra", Quota: 1},
		},
	}
	pool := domain.ComputePool{
		Metadata:  domain.Metadata{ID: "pool-1", Generation: 2},
		ProjectID: "project-1",
		Status:    domain.ResourceStatus{ObservedGeneration: 2, Conditions: []domain.Condition{{Type: "Ready", Status: "True"}}},
		Spec: domain.ComputePoolSpec{AcceleratorBindings: []domain.AcceleratorBinding{{
			Class:          "h100-dra",
			AllocationMode: domain.AcceleratorAllocationDRA,
			DRA:            &domain.DRAAllocation{DeviceClassName: "gpu.nvidia.com"},
		}}},
	}

	_, err := r.Reconcile(
		context.Background(),
		workload,
		pool,
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a", Provider: "provider-under-test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := fp.last.AcceleratorBinding
	if got.Class != "h100-dra" || got.DRA == nil || got.DRA.DeviceClassName != "gpu.nvidia.com" {
		t.Fatalf("provider-specific binding was not passed through: %#v", got)
	}
	if got.ResourceName != "" || got.Flavor != "" {
		t.Fatalf("generic reconciler invented provider-specific fields: %#v", got)
	}
}
