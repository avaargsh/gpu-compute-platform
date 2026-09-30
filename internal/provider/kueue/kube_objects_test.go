package kueue

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestJobObjectCarriesQueueAndGPURequest(t *testing.T) {
	job, err := jobObject(Job{
		Generation: 7,
		Name:       "job-train-1",
		Namespace:  "project-1",
		Image:      "example/train:latest",
		Command:    []string{"python", "train.py"},
		Resources:  map[string]int64{"vendor.example/gpu": 2},
		Labels: map[string]string{
			"kueue.x-k8s.io/queue-name": "lq-pool-h100",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Labels["kueue.x-k8s.io/queue-name"] != "lq-pool-h100" {
		t.Fatalf("queue label missing")
	}
	if job.Annotations[generationAnnotation] != "7" {
		t.Fatalf("desired generation annotation missing: %#v", job.Annotations)
	}
	got := job.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceName("vendor.example/gpu")]
	if got.Value() != 2 {
		t.Fatalf("gpu request=%d, want 2", got.Value())
	}
}

func TestClusterQueueObjectCarriesFlavorQuota(t *testing.T) {
	obj := clusterQueueObject(ClusterQueue{
		Name: "cq-pool-h100",
		Quotas: []ResourceQuota{
			{Flavor: "accel-h100-80g", Resource: "vendor.example/gpu", Nominal: 8},
		},
	})
	if obj.GetKind() != "ClusterQueue" || obj.GetName() != "cq-pool-h100" {
		t.Fatalf("unexpected object: %#v", obj.Object)
	}
	groups, found, err := unstructuredNestedSlice(obj.Object, "spec", "resourceGroups")
	if err != nil || !found || len(groups) != 1 {
		t.Fatalf("resource groups missing: %#v", obj.Object)
	}
}

func unstructuredNestedSlice(obj map[string]any, fields ...string) ([]any, bool, error) {
	current := any(obj)
	for i, field := range fields {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		next, ok := m[field]
		if !ok {
			return nil, false, nil
		}
		if i == len(fields)-1 {
			out, ok := next.([]any)
			return out, ok, nil
		}
		current = next
	}
	return nil, false, nil
}

func TestDRAObjectsUseStableResourceAPI(t *testing.T) {
	in := Job{
		Generation: 9,
		Name:       "job-train-dra", Namespace: "project-1", Image: "example/train:latest",
		Labels: map[string]string{"kueue.x-k8s.io/queue-name": "lq-pool-h100"},
		DRA:    &DRARequest{ClaimName: "accelerator-train-dra", DeviceClassName: "gpu.nvidia.com", Count: 2},
	}
	claim, err := resourceClaimObject(in)
	if err != nil {
		t.Fatal(err)
	}
	if claim.APIVersion != "resource.k8s.io/v1" || claim.Spec.Devices.Requests[0].Exactly.DeviceClassName != "gpu.nvidia.com" || claim.Spec.Devices.Requests[0].Exactly.Count != 2 {
		t.Fatalf("unexpected DRA claim: %#v", claim)
	}
	if claim.Annotations[generationAnnotation] != "9" {
		t.Fatalf("DRA claim generation annotation missing: %#v", claim.Annotations)
	}
	job, err := jobObject(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Spec.Template.Spec.ResourceClaims) != 1 || job.Spec.Template.Spec.ResourceClaims[0].ResourceClaimName == nil || *job.Spec.Template.Spec.ResourceClaims[0].ResourceClaimName != claim.Name {
		t.Fatalf("pod does not reference projected DRA claim: %#v", job.Spec.Template.Spec.ResourceClaims)
	}
	if job.Annotations[generationAnnotation] != "9" {
		t.Fatalf("DRA job generation annotation missing: %#v", job.Annotations)
	}
	if len(job.Spec.Template.Spec.Containers[0].Resources.Requests) != 0 {
		t.Fatalf("DRA workload must not also request an extended resource: %#v", job.Spec.Template.Spec.Containers[0].Resources.Requests)
	}
}
