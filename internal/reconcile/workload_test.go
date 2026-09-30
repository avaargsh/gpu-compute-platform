package reconcile

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type fakeWorkloadProvider struct {
	last        provider.WorkloadProjection
	calls       int
	deleteLast  provider.WorkloadProjection
	deleteCalls int
}

func (f *fakeWorkloadProvider) DeleteWorkload(_ context.Context, p provider.WorkloadProjection) (provider.DeletionObservation, error) {
	f.deleteCalls++
	f.deleteLast = p
	return provider.DeletionObservation{
		Gone: true,
		EvidenceRefs: []string{"k8s://cluster-a/namespaces/project-1/jobs/job-train-1"},
	}, nil
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


func TestWorkloadDeleteProjectsDRAReleaseAndPreservesEvidence(t *testing.T) {
	fp := &fakeWorkloadProvider{}
	r := NewWorkloadReconciler(fp)
	workload := domain.Workload{
		Metadata:  domain.Metadata{ID: "train-1", Generation: 3},
		ProjectID: "project-1",
		PoolID:    "pool-1",
		Spec: domain.WorkloadSpec{
			Image: "example/train:latest",
			Accelerator: domain.AcceleratorRequest{
				Class: "a100-mig-1g",
				Quota: 1,
			},
		},
	}
	pool := domain.ComputePool{
		Metadata:  domain.Metadata{ID: "pool-1", Generation: 9},
		ProjectID: "project-1",
		Spec: domain.ComputePoolSpec{
			AcceleratorBindings: []domain.AcceleratorBinding{{
				Class:          "a100-mig-1g",
				AllocationMode: domain.AcceleratorAllocationDRA,
				ResourceName:   "nvidia.com/gpu",
				Flavor:         "a100-mig-1g",
				DRA: &domain.DRAAllocation{
					DeviceClassName: "gpu.nvidia.com",
				},
			}},
		},
		Status: domain.ResourceStatus{
			ObservedGeneration: 1,
			Conditions: []domain.Condition{{
				Type: "Ready",
				Status: "False",
			}},
		},
	}

	got, err := r.Delete(
		context.Background(),
		workload,
		pool,
		domain.ProjectBinding{
			ProjectID: "project-1",
			ClusterID: "cluster-a",
			Namespace: "project-1",
		},
		domain.ClusterBinding{
			PoolID: "pool-1",
			ClusterID: "cluster-a",
			Provider: "kueue",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Gone {
		t.Fatalf("workload should be released: %#v", got)
	}
	if fp.deleteCalls != 1 {
		t.Fatalf("delete provider calls=%d, want 1", fp.deleteCalls)
	}
	if fp.deleteLast.AcceleratorBinding.AllocationMode != domain.AcceleratorAllocationDRA {
		t.Fatalf("DRA binding not preserved during release: %#v", fp.deleteLast.AcceleratorBinding)
	}
	if fp.deleteLast.Namespace != "project-1" || fp.deleteLast.ClusterID != "cluster-a" {
		t.Fatalf("release projection lost placement: %#v", fp.deleteLast)
	}
	if len(got.EvidenceRefs) != 1 {
		t.Fatalf("release evidence not preserved: %#v", got.EvidenceRefs)
	}
}

func TestWorkloadDeleteDoesNotRequireReadyPool(t *testing.T) {
	fp := &fakeWorkloadProvider{}
	r := NewWorkloadReconciler(fp)
	_, err := r.Delete(
		context.Background(),
		domain.Workload{
			Metadata: domain.Metadata{ID: "train-1"},
			ProjectID: "project-1",
			PoolID: "pool-1",
			Spec: domain.WorkloadSpec{
				Accelerator: domain.AcceleratorRequest{Class: "h100-80g", Quota: 1},
			},
		},
		domain.ComputePool{
			Metadata: domain.Metadata{ID: "pool-1", Generation: 2},
			ProjectID: "project-1",
			Spec: domain.ComputePoolSpec{
				AcceleratorBindings: []domain.AcceleratorBinding{{
					Class: "h100-80g",
					ResourceName: "nvidia.com/gpu",
					Flavor: "h100-80g",
				}},
			},
			Status: domain.ResourceStatus{
				ObservedGeneration: 1,
				Conditions: []domain.Condition{{Type: "Ready", Status: "False"}},
			},
		},
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if fp.deleteCalls != 1 {
		t.Fatalf("release must proceed for stale/not-ready pool, calls=%d", fp.deleteCalls)
	}
}
