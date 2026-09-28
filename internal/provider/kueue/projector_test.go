package kueue

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestProjectPool(t *testing.T) {
	got, err := ProjectPool(provider.PoolProjection{
		PoolID:     "pool-h100",
		ProjectID:  "project-1",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 3,
		Accelerators: []domain.AcceleratorRequest{
			{Class: "h100-80g", Quota: 8},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.ClusterQueue.Name != "cq-pool-h100" {
		t.Fatalf("unexpected cluster queue: %s", got.ClusterQueue.Name)
	}
	if got.LocalQueue.Namespace != "project-1" {
		t.Fatalf("unexpected namespace: %s", got.LocalQueue.Namespace)
	}
	if len(got.Flavors) != 1 || got.Flavors[0].ResourceName != "nvidia.com/gpu" {
		t.Fatalf("unexpected flavors: %#v", got.Flavors)
	}
	if len(got.ClusterQueue.Quotas) != 1 || got.ClusterQueue.Quotas[0].Nominal != 8 {
		t.Fatalf("unexpected quotas: %#v", got.ClusterQueue.Quotas)
	}
}

func TestProjectPoolRejectsInvalidQuota(t *testing.T) {
	_, err := ProjectPool(provider.PoolProjection{
		PoolID:       "pool-a",
		ClusterID:    "cluster-a",
		Namespace:    "project-a",
		Accelerators: []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 0}},
	})
	if err == nil {
		t.Fatal("expected invalid quota to fail")
	}
}
