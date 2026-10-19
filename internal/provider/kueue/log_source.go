package kueue

import (
	"context"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	logWorkloadIDAnnotation = "ai.compute/workload-id"
	logProjectIDAnnotation  = "ai.compute/project-id"
)

// WorkloadLogSource identifies one current Kubernetes Job/Pod pair. The
// cluster/project scope MUST come from a separately authorized management
// snapshot; this value is not an authorization grant or a log-read token.
// An eventual log reader MUST re-GET and compare both UIDs immediately before
// opening the Pod logs subresource, and recheck current project authorization.
type WorkloadLogSource struct {
	ClusterID          domain.ID
	ProjectID          domain.ID
	WorkloadID         domain.ID
	Generation         int64
	Namespace          string
	JobName            string
	JobUID             types.UID
	JobResourceVersion string
	PodName            string
	PodUID             types.UID
	PodResourceVersion string
	ContainerName      string
}

// InspectWorkloadLogSource is intentionally read-only and NOT wired to any
// HTTP route or Agent transport. An authorized caller supplies a server-owned
// WorkloadProjection, never browser-selected namespace, Job, Pod or container.
// Ambiguous/missing identities fail closed rather than selecting a random Pod.
func (c *KubeClient) InspectWorkloadLogSource(
	ctx context.Context, projection baseprovider.WorkloadProjection,
) (WorkloadLogSource, error) {
	if c == nil || c.core == nil {
		return WorkloadLogSource{}, fmt.Errorf("kubernetes read client is required")
	}
	if projection.ClusterID == "" || projection.ProjectID == "" ||
		projection.WorkloadID == "" || projection.Generation < 1 ||
		len(validation.IsDNS1123Label(projection.Namespace)) != 0 {
		return WorkloadLogSource{}, fmt.Errorf("authorized workload scope is incomplete")
	}
	// Existing Job naming replaces dots and underscores with hyphens. Until
	// exact workload identity is stamped on Job metadata, only canonical IDs
	// with a one-to-one mapping may be used to locate log source objects.
	if len(validation.IsDNS1123Label(string(projection.WorkloadID))) != 0 {
		return WorkloadLogSource{}, fmt.Errorf("workload ID cannot be mapped without aliasing")
	}
	jobName := resourceName("job", string(projection.WorkloadID))
	if len(validation.IsDNS1123Label(jobName)) != 0 {
		return WorkloadLogSource{}, fmt.Errorf("workload Job name is not a DNS label")
	}

	job, err := c.core.BatchV1().Jobs(projection.Namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		return WorkloadLogSource{}, fmt.Errorf("inspect workload Job: %w", err)
	}
	if job.Name != jobName || job.Namespace != projection.Namespace ||
		job.UID == "" || job.ResourceVersion == "" || job.DeletionTimestamp != nil ||
		job.Labels["ai.compute/workload"] != jobName ||
		job.Annotations[logWorkloadIDAnnotation] != string(projection.WorkloadID) ||
		job.Annotations[logProjectIDAnnotation] != string(projection.ProjectID) {
		return WorkloadLogSource{}, fmt.Errorf("workload Job identity is missing, stale or unowned")
	}
	if err := validateProviderGeneration("job", jobName, job.Annotations, projection.Generation); err != nil {
		return WorkloadLogSource{}, fmt.Errorf("workload Job generation mismatch: %w", err)
	}
	if !hasWorkloadContainer(job.Spec.Template.Spec.Containers) {
		return WorkloadLogSource{}, fmt.Errorf("workload Job container is missing")
	}

	// The label narrows the read set, but label equality never proves owner
	// identity. Require exactly one eligible Pod, with a controlling ownerRef
	// carrying the exact observed Job UID. Zero/multiple Pods are UNKNOWN.
	pods, err := c.core.CoreV1().Pods(projection.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "ai.compute/workload=" + jobName,
	})
	if err != nil {
		return WorkloadLogSource{}, fmt.Errorf("inspect workload Pods: %w", err)
	}
	if len(pods.Items) != 1 {
		return WorkloadLogSource{}, fmt.Errorf("workload Pod source is missing or ambiguous")
	}
	pod := &pods.Items[0]
	if pod.Namespace != projection.Namespace || pod.UID == "" ||
		pod.ResourceVersion == "" || pod.DeletionTimestamp != nil ||
		pod.Labels["ai.compute/workload"] != jobName ||
		!hasWorkloadContainer(pod.Spec.Containers) ||
		!podControlledByJob(pod.OwnerReferences, jobName, job.UID) {
		return WorkloadLogSource{}, fmt.Errorf("workload Pod identity is missing, stale or unowned")
	}
	return WorkloadLogSource{
		ClusterID: projection.ClusterID, ProjectID: projection.ProjectID,
		WorkloadID: projection.WorkloadID, Generation: projection.Generation,
		Namespace: projection.Namespace, JobName: jobName, JobUID: job.UID,
		JobResourceVersion: job.ResourceVersion, PodName: pod.Name,
		PodUID: pod.UID, PodResourceVersion: pod.ResourceVersion,
		ContainerName: "workload",
	}, nil
}

func hasWorkloadContainer(containers []corev1.Container) bool {
	found := 0
	for _, container := range containers {
		if container.Name == "workload" {
			found++
		}
	}
	return found == 1
}

func podControlledByJob(refs []metav1.OwnerReference, name string, uid types.UID) bool {
	controllers := 0
	for _, ref := range refs {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		controllers++
		if ref.APIVersion != "batch/v1" || ref.Kind != "Job" ||
			ref.Name != name || ref.UID != uid {
			return false
		}
	}
	return controllers == 1
}
