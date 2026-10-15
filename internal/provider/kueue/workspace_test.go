package kueue

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestWorkloadPVCMountedOnBothAllocationModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		dra  bool
	}{
		{name: "extended-resource"},
		{name: "dra", dra: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := Job{
				Generation: 1,
				Name: "job-train", Namespace: "project-a", Image: "example/train:v1",
				Workspace: &domain.WorkspaceMount{ClaimName: "training-data-pvc"},
				Resources: map[string]int64{"nvidia.com/gpu": 1},
			}
			if tc.dra {
				in.Resources = nil
				in.DRA = &DRARequest{ClaimName: "acc-train", DeviceClassName: "gpu.example", Count: 1}
			}
			job, err := jobObject(in)
			if err != nil {
				t.Fatal(err)
			}
			pod := job.Spec.Template.Spec
			if len(pod.Volumes) != 1 || pod.Volumes[0].PersistentVolumeClaim == nil ||
				pod.Volumes[0].PersistentVolumeClaim.ClaimName != "training-data-pvc" {
				t.Fatalf("workspace PVC volume missing: %#v", pod.Volumes)
			}
			if len(pod.Containers) != 1 || len(pod.Containers[0].VolumeMounts) != 1 ||
				pod.Containers[0].VolumeMounts[0].MountPath != "/workspace" {
				t.Fatalf("workspace mount missing: %#v", pod.Containers)
			}
			if tc.dra && len(pod.ResourceClaims) != 1 {
				t.Fatal("DRA ResourceClaim replaced by PVC workspace")
			}
		})
	}
}

func TestWorkspaceInvalidPVCFailsBeforeDRAApply(t *testing.T) {
	projection := baseprovider.WorkloadProjection{
		WorkloadID: "train-a", PoolID: "pool-a", Namespace: "project-a",
		Generation: 1, Image: "example/train:v1",
		Accelerator: domain.AcceleratorRequest{Class: "h100", Quota: 1},
		AcceleratorBinding: domain.AcceleratorBinding{Class: "h100", Flavor: "h100", ResourceName: "nvidia.com/gpu"},
		Workspace: &domain.WorkspaceMount{ClaimName: "../outside"},
	}
	if _, err := ProjectWorkload(projection); err == nil {
		t.Fatal("invalid PVC name admitted before Job and DRA side effects")
	}
	for _, bad := range []string{"", "a/b", "UPPER", "bad_name"} {
		_, err := jobObject(Job{
			Generation: 1, Name: "job-train", Namespace: "project-a", Image: "example/train:v1",
			Resources: map[string]int64{"nvidia.com/gpu": 1},
			Workspace: &domain.WorkspaceMount{ClaimName: bad},
		})
		if err == nil {
			t.Fatalf("accepted invalid PVC %q", bad)
		}
	}
}

func TestJobWithoutPVCWorkspaceUnchanged(t *testing.T) {
	job, err := jobObject(Job{
		Generation: 1, Name: "job-train", Namespace: "project-a", Image: "example/train:v1",
		Resources: map[string]int64{"nvidia.com/gpu": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Spec.Template.Spec.Volumes) != 0 {
		t.Fatalf("legacy Job unexpectedly gained a PVC: %#v", job.Spec.Template.Spec.Volumes)
	}
}

func TestProjectWorkloadCarriesWorkspaceToJob(t *testing.T) {
	job, err := ProjectWorkload(baseprovider.WorkloadProjection{
		WorkloadID: "train-a", PoolID: "pool-a", Namespace: "project-a",
		Generation: 1, Image: "example/train:v1",
		Accelerator: domain.AcceleratorRequest{Class: "h100", Quota: 1},
		AcceleratorBinding: domain.AcceleratorBinding{Class: "h100", Flavor: "h100", ResourceName: "nvidia.com/gpu"},
		Workspace: &domain.WorkspaceMount{ClaimName: "ws-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Workspace == nil || job.Workspace.ClaimName != "ws-1" {
		t.Fatalf("workspace lost at provider projection: %+v", job.Workspace)
	}
}
