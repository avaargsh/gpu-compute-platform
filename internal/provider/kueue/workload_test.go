package kueue

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestProjectWorkload(t *testing.T) {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.QueueName != "lq-pool-h100" {
		t.Fatalf("unexpected queue: %s", job.QueueName)
	}
	if job.Resources[gpuResourceName] != 2 {
		t.Fatalf("unexpected gpu request: %#v", job.Resources)
	}
	if job.Annotations["ai.compute/accelerator-class"] != "h100-80g" {
		t.Fatalf("accelerator class was not preserved")
	}
}
