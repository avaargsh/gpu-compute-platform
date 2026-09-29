package kueue

import (
	"context"
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

func TestDeleteWorkloadDoesNotRequireAcceleratorBinding(t *testing.T) {
	client := &fakeClient{}
	p := NewProvider(client)

	got, err := p.DeleteWorkload(context.Background(), baseprovider.WorkloadProjection{
		WorkloadID: "train-invalid",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Gone {
		t.Fatal("workload cleanup must observe job gone")
	}
	if len(client.order) != 1 || client.order[0] != "delete-job:project-1/job-train-invalid" {
		t.Fatalf("unexpected cleanup identity: %#v", client.order)
	}
}

func TestPodsReadyReasonDistinguishesTerminalJobs(t *testing.T) {
	tests := []struct {
		name  string
		state JobObservation
		want  string
	}{
		{name: "waiting", state: JobObservation{}, want: "AwaitingPods"},
		{name: "ready", state: JobObservation{PodsReady: true}, want: "PodsReady"},
		{name: "succeeded", state: JobObservation{Succeeded: true}, want: "JobCompleted"},
		{name: "failed", state: JobObservation{Failed: true}, want: "JobFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podsReadyReason(tt.state); got != tt.want {
				t.Fatalf("podsReadyReason()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestProjectDRAWorkloadUsesDeviceClassWithoutExtendedResource(t *testing.T) {
	got, err := ProjectWorkload(baseprovider.WorkloadProjection{
		WorkloadID: "workload-dra", PoolID: "pool-dra", Namespace: "project-1",
		Image: "example/train:latest", Accelerator: domain.AcceleratorRequest{Class: "h100-dra", Quota: 2},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class: "h100-dra", AllocationMode: domain.AcceleratorAllocationDRA, Flavor: "h100-dra",
			DRA: &domain.DRAAllocation{DeviceClassName: "gpu.nvidia.com"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DRA == nil || got.DRA.DeviceClassName != "gpu.nvidia.com" || got.DRA.Count != 2 {
		t.Fatalf("unexpected DRA projection: %#v", got.DRA)
	}
	if len(got.Resources) != 0 {
		t.Fatalf("DRA projection must not request extended resources: %#v", got.Resources)
	}
}

func TestProjectDRAWorkloadRequiresDeviceClass(t *testing.T) {
	_, err := ProjectWorkload(baseprovider.WorkloadProjection{
		WorkloadID: "workload-dra", PoolID: "pool-dra", Namespace: "project-1",
		Image: "example/train:latest", Accelerator: domain.AcceleratorRequest{Class: "h100-dra", Quota: 1},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class: "h100-dra", AllocationMode: domain.AcceleratorAllocationDRA, Flavor: "h100-dra",
		}},
	})
	if err == nil {
		t.Fatal("DRA projection must fail closed without a device class")
	}
}
