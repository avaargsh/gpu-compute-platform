package kueue

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestJobObjectCarriesQueueAndGPURequest(t *testing.T) {
	job, err := jobObject(Job{
		Name:      "job-train-1",
		Namespace: "project-1",
		Image:     "example/train:latest",
		Command:   []string{"python", "train.py"},
		Resources: map[string]int64{gpuResourceName: 2},
		Annotations: map[string]string{
			"kueue.x-k8s.io/queue-name": "lq-pool-h100",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Annotations["kueue.x-k8s.io/queue-name"] != "lq-pool-h100" {
		t.Fatalf("queue annotation missing")
	}
	got := job.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceName(gpuResourceName)]
	if got.Value() != 2 {
		t.Fatalf("gpu request=%d, want 2", got.Value())
	}
}

func TestClusterQueueObjectCarriesFlavorQuota(t *testing.T) {
	obj := clusterQueueObject(ClusterQueue{
		Name: "cq-pool-h100",
		Quotas: []ResourceQuota{
			{Flavor: "accel-h100-80g", Resource: gpuResourceName, Nominal: 8},
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
