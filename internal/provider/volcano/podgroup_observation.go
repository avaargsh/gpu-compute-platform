package volcano

import (
	"context"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PodGroup evidence is read-only. A matching PodGroup owner is a controller
// linkage, not gang admission, a scheduler-applied quota or an execution proof.
var volcanoPodGroupGVR = schema.GroupVersionResource{
	Group: "scheduling.volcano.sh", Version: "v1beta1", Resource: "podgroups",
}

type volcanoJobPodGroupReader interface {
	GetJobPodGroup(context.Context, *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

func (c *dynamicProjectedObjectClient) GetJobPodGroup(
	ctx context.Context, job *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	if c == nil || c.dynamic == nil || job == nil ||
		job.GetAPIVersion() != "batch.volcano.sh/v1alpha1" ||
		job.GetKind() != "Job" || job.GetName() == "" ||
		job.GetNamespace() == "" || job.GetUID() == "" {
		return nil, fmt.Errorf("PodGroup readback requires a UID-bound, namespaced Volcano Job")
	}
	name := job.GetName() + "-" + string(job.GetUID())
	return c.dynamic.Resource(volcanoPodGroupGVR).
		Namespace(job.GetNamespace()).Get(ctx, name, metav1.GetOptions{})
}

// observeOwnedPodGroupLink runs only after an independently Ready Pod is
// observed. A successful link is intentionally a separate condition: it
// must NOT change the Job Ready, Succeeded or Queue QuotaApplied semantics.
func (p *Provider) observeOwnedPodGroupLink(
	ctx context.Context, job *unstructured.Unstructured, now time.Time,
) (domain.Condition, error) {
	condition := domain.Condition{
		Type:               "PodGroupLinked",
		Status:             "Unknown",
		Reason:             "VolcanoPodGroupEvidenceUnavailable",
		Message:            "A UID-bound PodGroup controller link has not been proved",
		LastTransitionTime: now,
	}
	if job == nil || job.GetUID() == "" {
		condition.Reason = "VolcanoJobUIDUnavailable"
		return condition, nil
	}
	reader, ok := p.client.(volcanoJobPodGroupReader)
	if !ok {
		condition.Reason = "VolcanoPodGroupReaderUnavailable"
		return condition, nil
	}
	first, err := reader.GetJobPodGroup(ctx, job)
	if apierrors.IsNotFound(err) {
		condition.Reason = "VolcanoPodGroupNotFound"
		return condition, nil
	}
	if err != nil {
		return condition, fmt.Errorf("read Volcano PodGroup: %w", classifyProviderError(err))
	}
	if !podGroupOwnedByJob(first, job) {
		condition.Reason = "VolcanoPodGroupOwnershipUnproven"
		return condition, nil
	}
	second, err := reader.GetJobPodGroup(ctx, job)
	if apierrors.IsNotFound(err) {
		condition.Reason = "VolcanoPodGroupReadbackNotFound"
		return condition, nil
	}
	if err != nil {
		return condition, fmt.Errorf("re-read Volcano PodGroup: %w", classifyProviderError(err))
	}
	if !podGroupOwnedByJob(second, job) ||
		second.GetUID() != first.GetUID() ||
		second.GetResourceVersion() != first.GetResourceVersion() {
		condition.Reason = "VolcanoPodGroupReadbackDrift"
		return condition, nil
	}
	condition.Status = "True"
	condition.Reason = "VolcanoPodGroupControllerLinkVerified"
	condition.Message = "Two stable UID/RV-bound PodGroup GETs confirm Job-controller linkage, not scheduler admission or quota application"
	return condition, nil
}

func podGroupOwnedByJob(group, job *unstructured.Unstructured) bool {
	if group == nil || job == nil ||
		group.GetAPIVersion() != "scheduling.volcano.sh/v1beta1" ||
		group.GetKind() != "PodGroup" ||
		group.GetName() != job.GetName()+"-"+string(job.GetUID()) ||
		group.GetNamespace() != job.GetNamespace() ||
		group.GetUID() == "" || group.GetResourceVersion() == "" ||
		group.GetDeletionTimestamp() != nil {
		return false
	}
	jobQueue, qFound, qErr := unstructured.NestedString(job.Object, "spec", "queue")
	minAvailable, mFound, mErr := unstructured.NestedInt64(job.Object, "spec", "minAvailable")
	groupQueue, gqFound, gqErr := unstructured.NestedString(group.Object, "spec", "queue")
	minMember, gmFound, gmErr := unstructured.NestedInt64(group.Object, "spec", "minMember")
	if qErr != nil || !qFound || jobQueue == "" ||
		mErr != nil || !mFound || minAvailable != 1 ||
		gqErr != nil || !gqFound || groupQueue != jobQueue ||
		gmErr != nil || !gmFound || minMember != minAvailable {
		return false
	}
	controllerOwners := 0
	for _, owner := range group.GetOwnerReferences() {
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		controllerOwners++
		if owner.APIVersion != job.GetAPIVersion() ||
			owner.Kind != job.GetKind() || owner.Name != job.GetName() ||
			owner.UID != job.GetUID() {
			return false
		}
	}
	return controllerOwners == 1
}
