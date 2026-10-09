package volcano

import (
	"context"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// volcanoPodGVR is read-only in Stage B. The unregistered adapter never
// creates/updates/deletes Pods or infers quota application from them.
var volcanoPodGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

type volcanoJobPodLister interface {
	ListJobPods(context.Context, *unstructured.Unstructured) ([]unstructured.Unstructured, error)
	GetJobPod(context.Context, *unstructured.Unstructured, *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

func (c *dynamicProjectedObjectClient) ListJobPods(
	ctx context.Context, job *unstructured.Unstructured,
) ([]unstructured.Unstructured, error) {
	if c == nil || c.dynamic == nil || job == nil ||
		job.GetAPIVersion() != "batch.volcano.sh/v1alpha1" ||
		job.GetKind() != "Job" || job.GetName() == "" || job.GetNamespace() == "" {
		return nil, fmt.Errorf("Volcano Pod observation requires a namespaced Job and Kubernetes client")
	}
	selector := labels.Set{"volcano.sh/job-name": job.GetName()}.AsSelector().String()
	list, err := c.dynamic.Resource(volcanoPodGVR).
		Namespace(job.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// GetJobPod independently re-reads only the named Pod, never a name from
// untrusted command input. This is a read-only evidence fence, not a lock.
func (c *dynamicProjectedObjectClient) GetJobPod(
	ctx context.Context, job, pod *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	if c == nil || c.dynamic == nil || job == nil || pod == nil ||
		job.GetAPIVersion() != "batch.volcano.sh/v1alpha1" ||
		job.GetKind() != "Job" || job.GetName() == "" ||
		job.GetNamespace() == "" || pod.GetName() == "" ||
		pod.GetNamespace() != job.GetNamespace() {
		return nil, fmt.Errorf("Volcano Pod readback requires same-namespace named Job and Pod")
	}
	return c.dynamic.Resource(volcanoPodGVR).
		Namespace(job.GetNamespace()).Get(ctx, pod.GetName(), metav1.GetOptions{})
}

// observeOwnedPodReadiness is additional *read-only* evidence. The Job's
// Running phase is insufficient: Volcano only requires minAvailable Pods
// running, not that every projected Pod reports the Kubernetes Ready condition.
// Even PodsReady=True does NOT promote the adapter's Ready condition: PodGroup
// admission and scheduler quota application still lack independent proof.
func (p *Provider) observeOwnedPodReadiness(
	ctx context.Context, job *unstructured.Unstructured, now time.Time,
) (domain.Condition, error) {
	condition := domain.Condition{
		Type: "PodsReady", Status: "Unknown",
		Reason:             "OwnedPodEvidenceUnavailable",
		Message:            "No complete UID-bound Pod readiness observation",
		LastTransitionTime: now,
	}
	if job.GetUID() == "" {
		condition.Reason = "VolcanoJobUIDUnavailable"
		return condition, nil
	}
	lister, ok := p.client.(volcanoJobPodLister)
	if !ok {
		condition.Reason = "VolcanoPodObserverUnavailable"
		return condition, nil
	}
	pods, err := lister.ListJobPods(ctx, job)
	if err != nil {
		return condition, fmt.Errorf("list Volcano Job Pods: %w", classifyProviderError(err))
	}
	// The current projection intentionally emits one task with one replica.
	// Do not extend a 1/1 observation to an arbitrary future multi-Pod Job.
	tasks, found, err := unstructured.NestedSlice(job.Object, "spec", "tasks")
	if err != nil || !found || len(tasks) != 1 {
		condition.Reason = "UnreviewedVolcanoTaskShape"
		return condition, nil
	}
	task, ok := tasks[0].(map[string]any)
	if !ok {
		condition.Reason = "UnreviewedVolcanoTaskShape"
		return condition, nil
	}
	replicas, found, err := unstructured.NestedInt64(task, "replicas")
	if err != nil || !found || replicas != 1 || task["name"] != "workload" {
		condition.Reason = "UnreviewedVolcanoTaskShape"
		return condition, nil
	}
	if len(pods) != 1 {
		condition.Reason = "VolcanoPodCountUnproven"
		condition.Message = fmt.Sprintf("Expected 1 selected Pod, observed %d", len(pods))
		return condition, nil
	}
	pod := &pods[0]
	if !belongsToVolcanoJob(pod, job) {
		condition.Reason = "VolcanoPodOwnerMismatch"
		return condition, nil
	}
	if pod.GetDeletionTimestamp() != nil {
		condition.Status = "False"
		condition.Reason = "VolcanoPodTerminating"
		return condition, nil
	}
	if !podKubernetesReady(pod) {
		condition.Status = "False"
		condition.Reason = "VolcanoOwnedPodNotReady"
		return condition, nil
	}
	// LIST alone can report a Pod that was deleted/recreated or became
	// unready before the reconciliation finished. Only a fresh GET with
	// the same server UID and resourceVersion can corroborate this snapshot.
	// This still cannot prove the future state or scheduler/gang admission.
	if pod.GetResourceVersion() == "" {
		condition.Reason = "VolcanoPodResourceVersionUnavailable"
		return condition, nil
	}
	fresh, err := lister.GetJobPod(ctx, job, pod)
	if apierrors.IsNotFound(err) {
		condition.Reason = "VolcanoPodReadbackNotFound"
		return condition, nil
	}
	if err != nil {
		return condition, fmt.Errorf("read back Volcano Job Pod: %w", classifyProviderError(err))
	}
	if fresh == nil || fresh.GetUID() != pod.GetUID() ||
		fresh.GetResourceVersion() != pod.GetResourceVersion() ||
		!belongsToVolcanoJob(fresh, job) ||
		fresh.GetDeletionTimestamp() != nil || !podKubernetesReady(fresh) {
		condition.Reason = "VolcanoPodReadbackDrift"
		return condition, nil
	}
	condition.Status = "True"
	condition.Reason = "VolcanoOwnedPodReady"
	condition.Message = "One UID-bound Volcano Job Pod is Kubernetes Ready; PodGroup and scheduler quota remain unproven"
	return condition, nil
}

func belongsToVolcanoJob(pod, job *unstructured.Unstructured) bool {
	if pod.GetAPIVersion() != "v1" || pod.GetKind() != "Pod" ||
		pod.GetUID() == "" || pod.GetName() == "" ||
		pod.GetNamespace() != job.GetNamespace() ||
		pod.GetLabels()["volcano.sh/job-name"] != job.GetName() ||
		pod.GetLabels()["volcano.sh/job-namespace"] != job.GetNamespace() ||
		pod.GetLabels()["volcano.sh/task-spec"] != "workload" ||
		pod.GetLabels()["volcano.sh/task-index"] != "0" ||
		pod.GetAnnotations()["scheduling.k8s.io/group-name"] !=
			job.GetName()+"-"+string(job.GetUID()) {
		return false
	}
	for _, owner := range pod.GetOwnerReferences() {
		if owner.APIVersion == job.GetAPIVersion() && owner.Kind == job.GetKind() &&
			owner.Name == job.GetName() && owner.UID == job.GetUID() &&
			owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

func podKubernetesReady(pod *unstructured.Unstructured) bool {
	phase, found, err := unstructured.NestedString(pod.Object, "status", "phase")
	if err != nil || !found || phase != "Running" {
		return false
	}
	node, found, err := unstructured.NestedString(pod.Object, "spec", "nodeName")
	if err != nil || !found || node == "" {
		return false
	}
	conditions, found, err := unstructured.NestedSlice(pod.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	readyCount := 0
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		if condition["type"] == "Ready" {
			if condition["status"] != "True" {
				return false
			}
			readyCount++
		}
	}
	return readyCount == 1
}
