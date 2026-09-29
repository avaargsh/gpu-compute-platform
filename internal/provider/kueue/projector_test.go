package kueue

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestProjectPoolResolvesPortableAcceleratorBinding(t *testing.T) {
	got, err := ProjectPool(provider.PoolProjection{
		PoolID:     "pool-h100",
		ProjectID:  "project-1",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 3,
		Accelerators: []domain.AcceleratorRequest{
			{Class: "h100-80g", Quota: 8},
		},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class:        "h100-80g",
			ResourceName: "nvidia.com/gpu",
			Flavor:       "h100",
			NodeLabels: map[string]string{
				"nvidia.com/gpu.product":       "H100-SXM5-80GB",
				"topology.kubernetes.io/zone": "gpu-zone-a",
				"ai.compute/rack":              "rack-a01",
			},
		}},
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
	if len(got.Flavors) != 1 || got.Flavors[0].ResourceName != "nvidia.com/gpu" || got.Flavors[0].Name != "h100" {
		t.Fatalf("unexpected flavors: %#v", got.Flavors)
	}
	if got.Flavors[0].NodeLabels["nvidia.com/gpu.product"] != "H100-SXM5-80GB" ||
		got.Flavors[0].NodeLabels["topology.kubernetes.io/zone"] != "gpu-zone-a" ||
		got.Flavors[0].NodeLabels["ai.compute/rack"] != "rack-a01" {
		t.Fatalf("accelerator and topology selectors were not preserved: %#v", got.Flavors[0].NodeLabels)
	}
	if len(got.ClusterQueue.Quotas) != 1 || got.ClusterQueue.Quotas[0].Nominal != 8 {
		t.Fatalf("unexpected quotas: %#v", got.ClusterQueue.Quotas)
	}
}

func TestProjectPoolFailsClosedWithoutBinding(t *testing.T) {
	_, err := ProjectPool(provider.PoolProjection{
		PoolID:       "pool-a",
		ClusterID:    "cluster-a",
		Namespace:    "project-a",
		Accelerators: []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 8}},
	})
	if err == nil {
		t.Fatal("expected missing accelerator binding to fail")
	}
}

func TestProjectPoolRejectsInvalidQuota(t *testing.T) {
	_, err := ProjectPool(provider.PoolProjection{
		PoolID:       "pool-a",
		ClusterID:    "cluster-a",
		Namespace:    "project-a",
		Accelerators: []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 0}},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class: "h100-80g", ResourceName: "nvidia.com/gpu", Flavor: "h100",
		}},
	})
	if err == nil {
		t.Fatal("expected invalid quota to fail")
	}
}
