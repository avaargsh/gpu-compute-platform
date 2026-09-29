package kueue

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
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
					"coveredResources": coveredResources(in.Quotas),
					"flavors":          flavors,
				},
			},
		},
	}}
}

func coveredResources(quotas []ResourceQuota) []any {
	seen := make(map[string]struct{}, len(quotas))
	out := make([]any, 0, len(quotas))
	for _, quota := range quotas {
		if quota.Resource == "" {
			continue
		}
		if _, ok := seen[quota.Resource]; ok {
			continue
		}
		seen[quota.Resource] = struct{}{}
		out = append(out, quota.Resource)
	}
	return out
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
	if in.DRA != nil {
		if len(in.Resources) != 0 || in.DRA.ClaimName == "" || in.DRA.DeviceClassName == "" || in.DRA.Count <= 0 {
			return nil, fmt.Errorf("valid DRA request must not include extended resources")
		}
		jobLabels := cloneStringMap(in.Labels)
		jobLabels["ai.compute/workload"] = in.Name
		return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: in.Name, Namespace: in.Namespace, Annotations: cloneStringMap(in.Annotations), Labels: jobLabels}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"ai.compute/workload": in.Name}}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ResourceClaims: []corev1.PodResourceClaim{{Name: "accelerator", ResourceClaimName: &in.DRA.ClaimName}}, Containers: []corev1.Container{{Name: "workload", Image: in.Image, Command: append([]string(nil), in.Command...)}}}}}}, nil
	}
	if len(in.Resources) != 1 {
		return nil, fmt.Errorf("exactly one positive accelerator resource is required")
	}
	var resourceName string
	var count int64
	for name, value := range in.Resources {
		resourceName = name
		count = value
	}
	if resourceName == "" || count <= 0 {
		return nil, fmt.Errorf("exactly one positive accelerator resource is required")
	}
	qty := *resource.NewQuantity(count, resource.DecimalSI)
	jobLabels := cloneStringMap(in.Labels)
	jobLabels["ai.compute/workload"] = in.Name
	podLabels := map[string]string{"ai.compute/workload": in.Name}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        in.Name,
			Namespace:   in.Namespace,
			Annotations: cloneStringMap(in.Annotations),
			Labels:      jobLabels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "workload",
							Image:   in.Image,
							Command: append([]string(nil), in.Command...),
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceName(resourceName): qty,
								},
								Requests: corev1.ResourceList{
									corev1.ResourceName(resourceName): qty,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func resourceClaimObject(in Job) (*resourcev1.ResourceClaim, error) {
	if in.DRA == nil || in.DRA.ClaimName == "" || in.DRA.DeviceClassName == "" || in.DRA.Count <= 0 {
		return nil, fmt.Errorf("valid DRA request is required")
	}
	count := in.DRA.Count
	return &resourcev1.ResourceClaim{TypeMeta: metav1.TypeMeta{APIVersion: "resource.k8s.io/v1", Kind: "ResourceClaim"}, ObjectMeta: metav1.ObjectMeta{Name: in.DRA.ClaimName, Namespace: in.Namespace}, Spec: resourcev1.ResourceClaimSpec{Devices: resourcev1.DeviceClaim{Requests: []resourcev1.DeviceRequest{{Name: "accelerator", Exactly: &resourcev1.ExactDeviceRequest{DeviceClassName: in.DRA.DeviceClassName, AllocationMode: resourcev1.DeviceAllocationModeExactCount, Count: count}}}}}}, nil
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
