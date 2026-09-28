package kueue

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func resourceFlavorObject(in ResourceFlavor) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "ResourceFlavor",
		"metadata": map[string]any{
			"name": in.Name,
		},
		"spec": map[string]any{
			"nodeLabels": stringMapAny(in.NodeLabels),
		},
	}}
}

func clusterQueueObject(in ClusterQueue) *unstructured.Unstructured {
	flavors := make([]any, 0, len(in.Quotas))
	for _, q := range in.Quotas {
		flavors = append(flavors, map[string]any{
			"name": q.Flavor,
			"resources": []any{
				map[string]any{
					"name":         q.Resource,
					"nominalQuota": fmt.Sprintf("%d", q.Nominal),
				},
			},
		})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "ClusterQueue",
		"metadata": map[string]any{
			"name": in.Name,
		},
		"spec": map[string]any{
			"namespaceSelector": map[string]any{},
			"resourceGroups": []any{
				map[string]any{
					"coveredResources": []any{gpuResourceName},
					"flavors":          flavors,
				},
			},
		},
	}}
}

func localQueueObject(in LocalQueue) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "LocalQueue",
		"metadata": map[string]any{
			"name":      in.Name,
			"namespace": in.Namespace,
		},
		"spec": map[string]any{
			"clusterQueue": in.ClusterQueue,
		},
	}}
}

func jobObject(in Job) (*batchv1.Job, error) {
	gpu, ok := in.Resources[gpuResourceName]
	if !ok || gpu <= 0 {
		return nil, fmt.Errorf("positive gpu resource is required")
	}
	qty := *resource.NewQuantity(gpu, resource.DecimalSI)
	labels := map[string]string{"ai.compute/workload": in.Name}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        in.Name,
			Namespace:   in.Namespace,
			Annotations: cloneStringMap(in.Annotations),
			Labels:      labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "workload",
							Image:   in.Image,
							Command: append([]string(nil), in.Command...),
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceName(gpuResourceName): qty,
								},
								Requests: corev1.ResourceList{
									corev1.ResourceName(gpuResourceName): qty,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func stringMapAny(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

