package verifiedops

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	PODReadyObserved        = "POD_READY_OBSERVED"
	OutcomePending          = "OUTCOME_PENDING"
	OwnershipUnproven       = "OWNERSHIP_UNPROVEN"
	OutcomeMismatch         = "OUTCOME_MISMATCH"
	VerificationUnavailable = "VERIFICATION_UNAVAILABLE"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ExpectedJob is authority INPUT, not authority itself. Caller must obtain
// JobUID from a durable, authorized API-server CREATE binding (not by name).
type ExpectedJob struct {
	Namespace     string
	Name          string
	JobUID        types.UID
	Generation    int64
	ContainerName string
	ImageDigest   string
}

// Observation is a fresh, read-only Kubernetes correlation, not signed proof.
// POD_READY_OBSERVED must never be promoted to AUTHORIZED_AND_VERIFIED.
type Observation struct {
	Code                string
	JobUID              types.UID
	JobResourceVersion  string
	PodUID              types.UID
	PodResourceVersion  string
	NodeName            string
	NodeUID             types.UID
	NodeResourceVersion string
	ImageID             string
}

// JobObserver must be constructed with a separate Kubernetes read-only identity.
// It has no mutation method and is not wired into the write-side Reconciler.
type JobObserver struct{ client kubernetes.Interface }

func NewJobObserver(readOnlyClient kubernetes.Interface) *JobObserver {
	return &JobObserver{client: readOnlyClient}
}

// ValidExpectedJob is a read-only input validation predicate shared with
// the trusted PostgreSQL receipt admission. It does not grant authority.
func ValidExpectedJob(w ExpectedJob) bool { return validExpected(w) }

// ValidPinnedImageDigest refuses unpinned image claims in a v2 receipt.
func ValidPinnedImageDigest(d string) bool { return digestPattern.MatchString(d) }

func validExpected(w ExpectedJob) bool {
	return w.Namespace != "" && w.Name != "" && w.JobUID != "" && w.Generation > 0 &&
		w.ContainerName != "" && digestPattern.MatchString(w.ImageDigest)
}

func controllerOwnedBy(p *corev1.Pod, uid types.UID) bool {
	for _, r := range p.OwnerReferences {
		if r.APIVersion == "batch/v1" && r.Kind == "Job" && r.UID == uid &&
			r.Controller != nil && *r.Controller {
			return true
		}
	}
	return false
}

func readyPod(p *corev1.Pod) bool {
	if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || p.Spec.NodeName == "" {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func readyNode(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func jobIdentity(j *batchv1.Job, w ExpectedJob) bool {
	return j.UID == w.JobUID && j.ResourceVersion != "" &&
		j.Annotations["ai.compute/generation"] == strconv.FormatInt(w.Generation, 10) &&
		j.Labels["ai.compute/workload"] == w.Name
}

// Observe checks one pinned-image, single-Pod batch Job using fresh reads.
// Multiple Pods, missing provenance or API errors fail closed. This deliberately
// does not attest execution ownership, Kueue admission or deletion completion.
func (o *JobObserver) Observe(ctx context.Context, w ExpectedJob) (Observation, error) {
	if !validExpected(w) {
		return Observation{}, fmt.Errorf("expected bound Job UID, generation, container and sha256 digest are required")
	}
	if o == nil || o.client == nil {
		return Observation{}, fmt.Errorf("read-only Kubernetes client is required")
	}
	unavailable := Observation{Code: VerificationUnavailable}
	job, err := o.client.BatchV1().Jobs(w.Namespace).Get(ctx, w.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return unavailable, nil
	} // 404 != proved deletion
	if err != nil {
		return unavailable, err
	}
	if !jobIdentity(job, w) {
		return Observation{Code: OwnershipUnproven}, nil
	}
	if job.DeletionTimestamp != nil {
		return Observation{Code: OutcomePending}, nil
	}
	observed := Observation{Code: OutcomePending, JobUID: job.UID, JobResourceVersion: job.ResourceVersion}
	selector := labels.SelectorFromSet(labels.Set{"batch.kubernetes.io/controller-uid": string(job.UID)}).String()
	podList, err := o.client.CoreV1().Pods(w.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return unavailable, err
	}
	var owned []corev1.Pod
	for _, pod := range podList.Items {
		if controllerOwnedBy(&pod, job.UID) {
			owned = append(owned, pod)
		}
	}
	if len(owned) != 1 {
		return observed, nil
	} // not a single-Pod proof
	pod, err := o.client.CoreV1().Pods(w.Namespace).Get(ctx, owned[0].Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return unavailable, nil
	}
	if err != nil {
		return unavailable, err
	}
	if pod.UID == "" || pod.UID != owned[0].UID || pod.ResourceVersion == "" ||
		!controllerOwnedBy(pod, job.UID) || pod.Namespace != w.Namespace {
		return Observation{Code: OwnershipUnproven}, nil
	}
	observed.PodUID = pod.UID
	observed.PodResourceVersion = pod.ResourceVersion
	if !readyPod(pod) {
		return observed, nil
	}
	pinned := false
	for _, container := range job.Spec.Template.Spec.Containers {
		if container.Name == w.ContainerName && strings.HasSuffix(container.Image, "@"+w.ImageDigest) {
			pinned = true
		}
	}
	if !pinned {
		return Observation{Code: OutcomeMismatch}, nil
	}
	pinned = false
	for _, container := range pod.Spec.Containers {
		if container.Name == w.ContainerName && strings.HasSuffix(container.Image, "@"+w.ImageDigest) {
			pinned = true
		}
	}
	if !pinned {
		return Observation{Code: OutcomeMismatch}, nil
	}
	matched := false
	for _, state := range pod.Status.ContainerStatuses {
		if state.Name != w.ContainerName {
			continue
		}
		if !state.Ready {
			return observed, nil
		}
		if !strings.HasSuffix(state.ImageID, "@"+w.ImageDigest) {
			return Observation{Code: OutcomeMismatch}, nil
		}
		observed.ImageID = state.ImageID
		matched = true
	}
	if !matched {
		return observed, nil
	}
	node, err := o.client.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return unavailable, nil
	}
	if err != nil {
		return unavailable, err
	}
	if node.UID == "" || node.ResourceVersion == "" || !readyNode(node) {
		return observed, nil
	}
	observed.NodeUID = node.UID
	observed.NodeResourceVersion = node.ResourceVersion
	observed.NodeName = node.Name
	// Re-read both objects: an old LIST or a replaced Job cannot authorize
	// attribution of the new resource under a reused name.
	latestJob, err := o.client.BatchV1().Jobs(w.Namespace).Get(ctx, w.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return unavailable, nil
		}
		return unavailable, err
	}
	if !jobIdentity(latestJob, w) || latestJob.ResourceVersion != observed.JobResourceVersion {
		return Observation{Code: OwnershipUnproven}, nil
	}
	latestPod, err := o.client.CoreV1().Pods(w.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return unavailable, nil
		}
		return unavailable, err
	}
	if latestPod.UID != observed.PodUID || latestPod.ResourceVersion != observed.PodResourceVersion ||
		!controllerOwnedBy(latestPod, w.JobUID) || !readyPod(latestPod) {
		return Observation{Code: OwnershipUnproven}, nil
	}
	// Re-read Node too: a replaced or drained Node cannot authorize Ready.
	latestNode, err := o.client.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return unavailable, nil
	}
	if err != nil {
		return unavailable, err
	}
	if latestNode.UID != observed.NodeUID ||
		latestNode.ResourceVersion != observed.NodeResourceVersion || !readyNode(latestNode) {
		return Observation{Code: OwnershipUnproven}, nil
	}
	observed.Code = PODReadyObserved
	return observed, nil
}
