package kueue

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestProjectWorkloadUsesResolvedAcceleratorBinding(t *testing.T) {
	job, err := ProjectWorkload(baseprovider.WorkloadProjection{
		WorkloadID: "train-1",
		ProjectID:  "project-1",
		PoolID:     "pool-h100",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 2,
		Image:      "example/train:latest",
		Command:    []string{"python", "train.py"},
		Accelerator: domain.AcceleratorRequest{
			Class: "h100-80g",
			Quota: 2,
		},
		AcceleratorBinding: domain.AcceleratorBinding{
			Class: "h100-80g", ResourceName: "vendor.example/gpu", Flavor: "h100",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.QueueName != "lq-pool-h100" {
		t.Fatalf("unexpected queue: %s", job.QueueName)
	}
	if job.Resources["vendor.example/gpu"] != 2 {
		t.Fatalf("unexpected accelerator request: %#v", job.Resources)
	}
	if job.Annotations["ai.compute/accelerator-class"] != "h100-80g" {
		t.Fatal("accelerator class was not preserved")
	}
	if job.Annotations["ai.compute/accelerator-flavor"] != "h100" {
		t.Fatal("accelerator flavor was not preserved")
	}
}

func TestProjectWorkloadFailsClosedWithoutBinding(t *testing.T) {
	_, err := ProjectWorkload(baseprovider.WorkloadProjection{
		WorkloadID: "train-1",
		PoolID:     "pool-h100",
		Namespace:  "project-1",
		Image:      "example/train:latest",
		Accelerator: domain.AcceleratorRequest{
			Class: "h100-80g",
			Quota: 1,
		},
	})
	if err == nil {
		t.Fatal("expected missing accelerator binding to fail")
	}
}
