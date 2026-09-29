package kueue

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
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
	_, err = claims.Get(ctx, claim.Name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = claims.Create(ctx, claim, metav1.CreateOptions{})
	return err
}

func (c *KubeClient) ApplyJob(ctx context.Context, in Job) error {
	if c.core == nil {
		return fmt.Errorf("kubernetes client is required")
	}
	job, err := jobObject(in)
	if err != nil {
		return err
	}

	jobs := c.core.BatchV1().Jobs(in.Namespace)
	_, err = jobs.Get(ctx, in.Name, metav1.GetOptions{})
	if err == nil {
		// Jobs are immutable execution objects. Re-applying an existing Job with
		// Update would also overwrite controller-owned status on every agent tick.
		// Desired generation changes must use an explicit replacement strategy.
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = jobs.Create(ctx, job, metav1.CreateOptions{})
	return err
}

func (c *KubeClient) ObserveJob(ctx context.Context, namespace, name string) (JobObservation, error) {
	if c.core == nil {
		return JobObservation{}, fmt.Errorf("kubernetes client is required")
	}
	job, err := c.core.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return JobObservation{}, err
	}

	out := JobObservation{Phase: "Pending"}
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
				if !ownedByJob(workload.GetOwnerReferences(), job.UID) {
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
		if podReady(pod.Status.Conditions) {
			out.PodsReady = true
			if !out.Failed && !out.Succeeded {
				out.Phase = "Running"
			}
			break
		}
	}
	return out, nil
}

func (c *KubeClient) DeleteJob(ctx context.Context, namespace, name string) (bool, error) {
	if c.core == nil {
		return false, fmt.Errorf("kubernetes client is required")
	}
	jobs := c.core.BatchV1().Jobs(namespace)
	err := jobs.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	_, err = jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (c *KubeClient) DeleteResourceClaim(ctx context.Context, namespace, name string) (bool, error) {
	if c.core == nil {
		return false, fmt.Errorf("kubernetes client is required")
	}
	claims := c.core.ResourceV1().ResourceClaims(namespace)
	err := claims.Delete(ctx, name, metav1.DeleteOptions{})
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

func ownedByJob(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.Kind == "Job" && ref.UID == uid {
			return true
		}
	}
	return false
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
