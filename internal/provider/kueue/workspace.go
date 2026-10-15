package kueue

import (
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const workspaceVolumeName = "workspace"
const workspaceMountPath = "/workspace"

// jobObject adds an existing same-namespace PVC to both extended-resource and
// DRA workloads. The Workload owns the Job, not the PVC or its storage data.
func jobObject(in Job) (*batchv1.Job, error) {
	if in.Workspace != nil && len(validation.IsDNS1123Subdomain(in.Workspace.ClaimName)) != 0 {
		return nil, fmt.Errorf("workspace claimName must be a valid PVC name")
	}
	job, err := buildJobObject(in)
	if err != nil {
		return nil, err
	}
	if in.Workspace == nil {
		return job, nil
	}
	applyWorkspace(job, in.Workspace)
	return job, nil
}

func applyWorkspace(job *batchv1.Job, mount *domain.WorkspaceMount) {
	pod := &job.Spec.Template.Spec
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: workspaceVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: mount.ClaimName,
			},
		},
	})
	for i := range pod.Containers {
		pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts,
			corev1.VolumeMount{Name: workspaceVolumeName, MountPath: workspaceMountPath})
	}
}
