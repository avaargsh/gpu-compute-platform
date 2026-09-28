package kueue

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var (
	resourceFlavorGVR = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "resourceflavors"}
	clusterQueueGVR   = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "clusterqueues"}
	localQueueGVR     = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "localqueues"}
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

func (c *KubeClient) ApplyJob(ctx context.Context, in Job) error {
	if c.core == nil {
		return fmt.Errorf("kubernetes client is required")
	}
	job, err := jobObject(in)
	if err != nil {
		return err
	}

	jobs := c.core.BatchV1().Jobs(in.Namespace)
	current, err := jobs.Get(ctx, in.Name, metav1.GetOptions{})
	if err == nil {
		job.ResourceVersion = current.ResourceVersion
		_, err = jobs.Update(ctx, job, metav1.UpdateOptions{})
		return err
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
		return out, nil
	}
	if job.Status.Succeeded > 0 {
		out.Phase = "Succeeded"
		out.PodsReady = true
		return out, nil
	}
	if job.Status.Active > 0 {
		out.Phase = "Running"
		out.Admitted = true
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
			out.Admitted = true
			out.Phase = "Running"
			break
		}
	}
	return out, nil
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

func podReady(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
