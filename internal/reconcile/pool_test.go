package reconcile

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type fakeComputeProvider struct {
	last provider.PoolProjection
}

func (f *fakeComputeProvider) DeletePool(_ context.Context, _ provider.PoolProjection) (provider.DeletionObservation, error) {
	return provider.DeletionObservation{Gone: true}, nil
}

func (f *fakeComputeProvider) ReconcilePool(_ context.Context, p provider.PoolProjection) (provider.PoolObservation, error) {
	f.last = p
	return provider.PoolObservation{
		ObservedGeneration: p.Generation,
		Conditions: []domain.Condition{
			{Type: "Ready", Status: "True", Reason: "Reconciled"},
		},
		EvidenceRefs: []string{"k8s://cluster-a/clusterqueue/cq-pool-1"},
	}, nil
}

func TestPoolReconcilerBuildsProjection(t *testing.T) {
	fp := &fakeComputeProvider{}
	r := NewPoolReconciler(fp)

	pool := domain.ComputePool{
		Metadata:  domain.Metadata{ID: "pool-1", Generation: 4},
		ProjectID: "project-1",
		Spec: domain.ComputePoolSpec{
			Accelerators: []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 8}},
		},
	}
	status, evidence, err := r.Reconcile(context.Background(), pool,
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-a", Provider: "kueue"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if fp.last.Generation != 4 || fp.last.Namespace != "project-1" {
		t.Fatalf("unexpected projection: %#v", fp.last)
	}
	if status.ObservedGeneration != 4 || len(evidence) != 1 {
		t.Fatalf("unexpected observation: %#v %#v", status, evidence)
	}
}

func TestPoolReconcilerRejectsCrossClusterBindings(t *testing.T) {
	r := NewPoolReconciler(&fakeComputeProvider{})
	_, _, err := r.Reconcile(context.Background(),
		domain.ComputePool{Metadata: domain.Metadata{ID: "pool-1"}, ProjectID: "project-1"},
		domain.ProjectBinding{ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "p1"},
		domain.ClusterBinding{PoolID: "pool-1", ClusterID: "cluster-b"},
	)
	if err == nil {
		t.Fatal("expected cross-cluster bindings to fail")
	}
}
