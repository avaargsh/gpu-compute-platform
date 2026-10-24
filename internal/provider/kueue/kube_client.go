package kueue

import (
	"context"
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

var (
	resourceFlavorGVR = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "resourceflavors"}
	clusterQueueGVR   = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "clusterqueues"}
	localQueueGVR     = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "localqueues"}
	workloadGVR       = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "workloads"}
)

type KubeClient struct {
	core    kubernetes.Interface
	dynamic dynamic.Interface
}

func NewKubeClient(core kubernetes.Interface, dynamicClient dynamic.Interface) *KubeClient {
	return &KubeClient{core: core, dynamic: dynamicClient}
}

func (c *KubeClient) ApplyResourceFlavor(ctx context.Context, in ResourceFlavor) error {
	return c.apply(ctx, resourceFlavorGVR, "", resourceFlavorObject(in))
}

func (c *KubeClient) ApplyClusterQueue(ctx context.Context, in ClusterQueue) error {
	return c.apply(ctx, clusterQueueGVR, "", clusterQueueObject(in))
}

func (c *KubeClient) ApplyLocalQueue(ctx context.Context, in LocalQueue) error {
	return c.apply(ctx, localQueueGVR, in.Namespace, localQueueObject(in))
}

func (c *KubeClient) ApplyResourceClaim(ctx context.Context, in Job) error {
	if in.DRA == nil {
		return nil
	}
	if c.core == nil {
		return fmt.Errorf("kubernetes client is required")
	}
	claim, err := resourceClaimObject(in)
	if err != nil {
		return err
	}
	claims := c.core.ResourceV1().ResourceClaims(in.Namespace)
	current, err := claims.Get(ctx, claim.Name, metav1.GetOptions{})
	if err == nil {
		return validateProviderGeneration("resource claim", claim.Name, current.Annotations, in.Generation)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = claims.Create(ctx, claim, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	// The lease only fences who may start reconciliation; it cannot make an
	// external Kubernetes CREATE exactly-once. If another owner completed the
	// same deterministic side effect after our GET but before our CREATE, adopt
	// the object only when its desired generation matches.
	current, err = claims.Get(ctx, claim.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("adopt resource claim %s after create race: %w", claim.Name, err)
	}
	return validateProviderGeneration("resource claim", claim.Name, current.Annotations, in.Generation)
}

// JobApplyDisposition distinguishes a response to our actual API-server
// CREATE from deterministic same-generation convergence after GET/AlreadyExists.
// Only CreateAcknowledged may carry the CREATE response UID/RV. Neither status
// independently establishes original Management operation ownership.
type JobApplyDisposition string

const (
	JobCreateAcknowledged JobApplyDisposition = "CREATE_ACKNOWLEDGED"
	JobExistingUnbound    JobApplyDisposition = "EXISTING_UNBOUND"
	JobRaceAdoptedUnbound JobApplyDisposition = "RACE_ADOPTED_UNBOUND"
)

type JobCreateResponse struct {
	Disposition     JobApplyDisposition
	UID             types.UID
	ResourceVersion string
}

// ApplyJob preserves the existing provider's at-least-once reconciliation
// semantics. It never publishes a CREATE response or invents operation proof.
func (c *KubeClient) ApplyJob(ctx context.Context, in Job) error {
	_, err := c.applyJobWithResponse(ctx, in, nil)
	return err
}

// ApplyJobWithCreateResponse is the bounded API-server CREATE response
// capture seam for a future authenticated Agent / durable Operation binding.
// An existing or raced object returns NO UID/RV even when its GET has them.
// A successful CREATE without API-server-issued UID/RV fails closed here,
// while legacy ApplyJob still performs ordinary provider convergence.
func (c *KubeClient) ApplyJobWithCreateResponse(ctx context.Context, in Job) (JobCreateResponse, error) {
	result, err := c.applyJobWithResponse(ctx, in, nil)
	if err != nil {
		return JobCreateResponse{}, err
	}
	if result.Disposition == JobCreateAcknowledged && (result.UID == "" || result.ResourceVersion == "") {
		return JobCreateResponse{}, fmt.Errorf("job CREATE response missing API-server UID or resourceVersion")
	}
	return result, nil
}

// Guarded CREATE owns the operation-scoped provider mutation sequence: read the
// Job, commit the durable PRE-CREATE guard, create any prerequisite DRA claim,
// then issue the Job CREATE. No provider writes may occur before the guard.
func (c *KubeClient) ApplyJobWithGuardedCreate(ctx context.Context, in Job, before func(context.Context) error) (JobCreateResponse, error) {
	if before == nil {
		return JobCreateResponse{}, fmt.Errorf("durable pre-CREATE guard required")
	}
	return c.applyJobWithResponse(ctx, in, before)
}

// InspectBoundJob is strictly read-only. It permits a previously ACK-bound
// operation to resume observation only while the same API-server UID still
// occupies the deterministic resource name. It never adopts by name or
// launches a replacement when the old Job is absent.
func (c *KubeClient) InspectBoundJob(ctx context.Context, in Job, expectedUID string) error {
	if c.core == nil {
		return fmt.Errorf("kubernetes client is required")
	}
	if expectedUID == "" || in.Namespace == "" || in.Name == "" || in.Generation < 1 {
		return verifiedops.ErrCreateUIDUnproven
	}
	current, err := c.core.BatchV1().Jobs(in.Namespace).Get(ctx, in.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%w: ACK-bound Job is absent", verifiedops.ErrCreateUIDUnproven)
	}
	if err != nil {
		return err
	}
	if current.Namespace != in.Namespace || current.Name != in.Name ||
		string(current.UID) != expectedUID || current.ResourceVersion == "" {
		return fmt.Errorf("%w: live Job identity differs from durable CREATE ACK", verifiedops.ErrCreateUIDUnproven)
	}
	if err := validateProviderGeneration("job", in.Name, current.Annotations, in.Generation); err != nil {
		return fmt.Errorf("%w: %v", verifiedops.ErrCreateUIDUnproven, err)
	}
	return nil
}

func (c *KubeClient) applyJobWithResponse(ctx context.Context, in Job, before func(context.Context) error) (JobCreateResponse, error) {
	if c.core == nil {
		return JobCreateResponse{}, fmt.Errorf("kubernetes client is required")
	}
	job, err := jobObject(in)
	if err != nil {
		return JobCreateResponse{}, err
	}

	jobs := c.core.BatchV1().Jobs(in.Namespace)
	current, err := jobs.Get(ctx, in.Name, metav1.GetOptions{})
	if err == nil {
		// Existing objects may be adopted for convergence, but their UID
		// was not returned by our CREATE and must never be claimed as such.
		if err := validateProviderGeneration("job", in.Name, current.Annotations, in.Generation); err != nil {
			return JobCreateResponse{}, err
		}
		return JobCreateResponse{Disposition: JobExistingUnbound}, nil
	}
	if !apierrors.IsNotFound(err) {
		return JobCreateResponse{}, err
	}

	if before != nil {
		if err := before(ctx); err != nil {
			return JobCreateResponse{}, fmt.Errorf("durable CREATE attempt denied: %w", err)
		}
		// A ResourceClaim is itself a Kubernetes side effect. Keep it behind the
		// durable guard so UNKNOWN/STALE/refused attempts cannot mutate the
		// cluster even when the Job CREATE is correctly blocked.
		if in.DRA != nil {
			if err := c.ApplyResourceClaim(ctx, in); err != nil {
				return JobCreateResponse{}, fmt.Errorf("apply DRA ResourceClaim after durable CREATE preparation: %w", err)
			}
		}
	}
	created, err := jobs.Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		if created == nil {
			return JobCreateResponse{Disposition: JobCreateAcknowledged}, nil
		}
		return JobCreateResponse{Disposition: JobCreateAcknowledged, UID: created.UID, ResourceVersion: created.ResourceVersion}, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		// A lost acknowledgement or timeout is UNKNOWN, never a CREATE
		// response suitable for binding. Preserve the error for recovery.
		return JobCreateResponse{}, err
	}

	// A race winner may have performed the same deterministic side effect.
	// Generation-safe adoption remains legal but never yields a CREATE UID.
	current, err = jobs.Get(ctx, in.Name, metav1.GetOptions{})
	if err != nil {
		return JobCreateResponse{}, fmt.Errorf("adopt job %s after create race: %w", in.Name, err)
	}
	if err := validateProviderGeneration("job", in.Name, current.Annotations, in.Generation); err != nil {
		return JobCreateResponse{}, err
	}
	return JobCreateResponse{Disposition: JobRaceAdoptedUnbound}, nil
}

func validateProviderGeneration(kind, name string, annotations map[string]string, generation int64) error {
	wantGeneration := strconv.FormatInt(generation, 10)
	haveGeneration := ""
	if annotations != nil {
		haveGeneration = annotations[generationAnnotation]
	}
	if haveGeneration != wantGeneration {
		return fmt.Errorf("%s %s generation drift: have %q want %q", kind, name, haveGeneration, wantGeneration)
	}
	return nil
}

func (c *KubeClient) ObserveJob(ctx context.Context, namespace, name string) (JobObservation, error) {
	if c.core == nil {
		return JobObservation{}, fmt.Errorf("kubernetes client is required")
	}
	job, err := c.core.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return JobObservation{}, err
	}

	out := JobObservation{Phase: "Pending", JobUID: string(job.UID)}
	if job.Status.Failed > 0 {
		out.Phase = "Failed"
		out.Failed = true
		out.Message = "job has failed pods"
	} else if job.Status.Succeeded > 0 {
		out.Phase = "Succeeded"
		out.Succeeded = true
	} else if job.Status.Active > 0 {
		out.Phase = "Running"
	}

	if c.dynamic != nil {
		workloads, err := c.dynamic.Resource(workloadGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return JobObservation{}, err
			}
		} else {
			for _, workload := range workloads.Items {
				if !ownedByJob(workload.GetOwnerReferences(), job) {
					continue
				}
				out.WorkloadName = workload.GetName()
				conditions, found, err := unstructured.NestedSlice(workload.Object, "status", "conditions")
				if err != nil {
					return JobObservation{}, err
				}
				if found {
					out.QuotaReserved = conditionTrue(conditions, "QuotaReserved")
					out.Admitted = conditionTrue(conditions, "Admitted")
				}
				break
			}
		}
	}

	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "ai.compute/workload=" + name,
	})
	if err != nil {
		return JobObservation{}, err
	}
	for _, pod := range pods.Items {
		if readyPodOwnedByJob(pod, job) {
			out.PodsReady = true
			if !out.Failed && !out.Succeeded {
				out.Phase = "Running"
			}
			break
		}
	}
	return out, nil
}

// Ordinary status is not proof of execution ownership, but even a Ready
// signal must be attributable to the exact API-server Job UID. Labels alone
// can be forged by another Pod or left behind after name reuse.
func readyPodOwnedByJob(pod corev1.Pod, job *batchv1.Job) bool {
	if job == nil || job.UID == "" || pod.DeletionTimestamp != nil ||
		pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" ||
		!podReady(pod.Status.Conditions) {
		return false
	}
	for _, owner := range pod.OwnerReferences {
		if owner.APIVersion == "batch/v1" && owner.Kind == "Job" &&
			owner.Name == job.Name && owner.UID == job.UID &&
			owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

// Guarded workload deletion verifies generation/ownership before applying
// Kubernetes UID+resourceVersion compare-and-delete. The agent lease does not
// fence an older already-in-flight provider DELETE.
func (c *KubeClient) DeleteJob(ctx context.Context, namespace, name string, generation int64) (bool, error) {
	if c.core == nil {
		return false, fmt.Errorf("kubernetes client is required")
	}
	if namespace == "" || name == "" || generation < 1 {
		return false, fmt.Errorf("job deletion requires namespace, name and positive generation")
	}
	jobs := c.core.BatchV1().Jobs(namespace)
	current, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// A Job can disappear before a Pod with its workload label has actually
		// vanished (for example after an orphaned/dependent Pod survives GC).
		// The Workload finalizer is released only after the independent Pod list
		// is empty; a 404 for the Job alone is not a release proof.
		return c.workloadPodsAbsent(ctx, namespace, name)
	}
	if err != nil {
		return false, err
	}
	if err := validateProviderGeneration("job", name, current.Annotations, generation); err != nil {
		return false, err
	}
	if current.Labels["ai.compute/workload"] != name {
		return false, fmt.Errorf("job %s owner identity mismatch", name)
	}
	preconditions, err := workloadDeletePreconditions(string(current.UID), current.ResourceVersion)
	if err != nil {
		return false, fmt.Errorf("job %s: %w", name, err)
	}
	propagation := metav1.DeletePropagationForeground
	err = jobs.Delete(ctx, name, metav1.DeleteOptions{
		Preconditions:     preconditions,
		PropagationPolicy: &propagation,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	// DELETE ACK alone is never proof of absence; a replacement with the
	// same name must not be mistaken for this generation having completed.
	_, err = jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return c.workloadPodsAbsent(ctx, namespace, name)
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// workloadPodsAbsent is a fail-closed release fence. Kubernetes foreground
// deletion normally removes Job-owned dependents before the Job disappears,
// but a 404 for the owner is insufficient evidence if a workload-labelled Pod
// remains. Do not delete by label alone; reconciliation must wait or surface
// the inconsistent Provider state for review.
func (c *KubeClient) workloadPodsAbsent(ctx context.Context, namespace, jobName string) (bool, error) {
	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "ai.compute/workload=" + jobName,
	})
	if err != nil {
		return false, err
	}
	if len(pods.Items) != 0 {
		return false, nil
	}
	return true, nil
}

// Claim deletion requires the same exact Job identity written by the reviewed
// DRA projector. Legacy unlabeled claims are retained for manual migration.
func (c *KubeClient) DeleteResourceClaim(ctx context.Context, namespace, name, jobName string, generation int64) (bool, error) {
	if c.core == nil {
		return false, fmt.Errorf("kubernetes client is required")
	}
	if namespace == "" || name == "" || jobName == "" || generation < 1 {
		return false, fmt.Errorf("claim deletion requires namespace, name, job and positive generation")
	}
	claims := c.core.ResourceV1().ResourceClaims(namespace)
	current, err := claims.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := validateProviderGeneration("resource claim", name, current.Annotations, generation); err != nil {
		return false, err
	}
	if current.Labels["ai.compute/workload"] != jobName {
		return false, fmt.Errorf("resource claim %s owner identity mismatch", name)
	}
	preconditions, err := workloadDeletePreconditions(string(current.UID), current.ResourceVersion)
	if err != nil {
		return false, fmt.Errorf("resource claim %s: %w", name, err)
	}
	err = claims.Delete(ctx, name, metav1.DeleteOptions{Preconditions: preconditions})
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	_, err = claims.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func workloadDeletePreconditions(uid, version string) (*metav1.Preconditions, error) {
	if uid == "" || version == "" {
		return nil, fmt.Errorf("UID and resourceVersion are required for provider deletion")
	}
	id := types.UID(uid)
	return &metav1.Preconditions{UID: &id, ResourceVersion: &version}, nil
}

func (c *KubeClient) DeleteResourceFlavor(ctx context.Context, name string) (bool, error) {
	return c.deleteDynamic(ctx, resourceFlavorGVR, "", name)
}

func (c *KubeClient) DeleteClusterQueue(ctx context.Context, name string) (bool, error) {
	return c.deleteDynamic(ctx, clusterQueueGVR, "", name)
}

func (c *KubeClient) DeleteLocalQueue(ctx context.Context, namespace, name string) (bool, error) {
	return c.deleteDynamic(ctx, localQueueGVR, namespace, name)
}

func (c *KubeClient) deleteDynamic(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (bool, error) {
	if c.dynamic == nil {
		return false, fmt.Errorf("dynamic kubernetes client is required")
	}
	resource := c.dynamic.Resource(gvr)
	if namespace != "" {
		ns := resource.Namespace(namespace)
		err := ns.Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		_, err = ns.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}
	err := resource.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	_, err = resource.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (c *KubeClient) apply(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured) error {
	if c.dynamic == nil {
		return fmt.Errorf("dynamic kubernetes client is required")
	}
	resource := c.dynamic.Resource(gvr)
	if namespace != "" {
		current, err := resource.Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err == nil {
			obj.SetResourceVersion(current.GetResourceVersion())
			_, err = resource.Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
			return err
		}
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err = resource.Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
		return err
	}

	current, err := resource.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if err == nil {
		obj.SetResourceVersion(current.GetResourceVersion())
		_, err = resource.Update(ctx, obj, metav1.UpdateOptions{})
		return err
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = resource.Create(ctx, obj, metav1.CreateOptions{})
	return err
}

// Matching a Job UID alone does not establish controller ownership of a
// Kueue Workload. This is a scheduling-evidence attribution fence, NOT proof
// that an authorized operation issued the original Kubernetes CREATE.
func ownedByJob(refs []metav1.OwnerReference, job *batchv1.Job) bool {
	if job == nil || job.UID == "" || job.Name == "" {
		return false
	}
	controllers := 0
	for _, ref := range refs {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		controllers++
		if ref.APIVersion != "batch/v1" || ref.Kind != "Job" ||
			ref.Name != job.Name || ref.UID != job.UID {
			return false
		}
	}
	return controllers == 1
}

func conditionTrue(conditions []any, conditionType string) bool {
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if condition["type"] == conditionType && condition["status"] == "True" {
			return true
		}
	}
	return false
}

func podReady(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
