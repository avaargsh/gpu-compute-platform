package volcano

import (
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func adoptionPoolProjection() baseprovider.PoolProjection {
	return baseprovider.PoolProjection{
		Provider:   ProviderName,
		PoolID:     "pool-h100",
		ProjectID:  "project-a",
		ClusterID:  "cluster-a",
		Namespace:  "project-a",
		Generation: 4,
		Accelerators: []domain.AcceleratorRequest{
			{Class: "h100-80g", Quota: 8},
		},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class:          "h100-80g",
			AllocationMode: domain.AcceleratorAllocationExtendedResource,
			ResourceName:   "nvidia.com/gpu",
		}},
	}
}

func adoptionWorkloadProjection() baseprovider.WorkloadProjection {
	return baseprovider.WorkloadProjection{
		Provider:   ProviderName,
		WorkloadID: "train-one",
		ProjectID:  "project-a",
		PoolID:     "pool-h100",
		ClusterID:  "cluster-a",
		Namespace:  "project-a",
		Generation: 7,
		Image:      "example/train:stable",
		Command:    []string{"python", "train.py"},
		Accelerator: domain.AcceleratorRequest{
			Class: "h100-80g",
			Quota: 2,
		},
		AcceleratorBinding: domain.AcceleratorBinding{
			Class:          "h100-80g",
			AllocationMode: domain.AcceleratorAllocationExtendedResource,
			ResourceName:   "nvidia.com/gpu",
		},
	}
}

func TestClassifyExistingObjectCreatesWhenDeterministicIdentityIsAbsent(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	action, err := classifyExistingObject(expected, nil)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectCreate {
		t.Fatalf("action=%q, want %q", action, existingObjectCreate)
	}
}

func TestClassifyExistingObjectRejectsMalformedExpectedIdentityBeforeCreate(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}

	unsupported := expected.DeepCopy()
	unsupported.SetKind("ConfigMap")
	if _, err := classifyExistingObject(unsupported, nil); err == nil {
		t.Fatal("unsupported provider object kind must fail before create")
	}

	missingGeneration := expected.DeepCopy()
	annotations := missingGeneration.GetAnnotations()
	delete(annotations, generationAnnotation)
	missingGeneration.SetAnnotations(annotations)
	if _, err := classifyExistingObject(missingGeneration, nil); err == nil {
		t.Fatal("missing expected generation marker must fail before create")
	}

	missingSpec := expected.DeepCopy()
	delete(missingSpec.Object, "spec")
	if _, err := classifyExistingObject(missingSpec, nil); err == nil {
		t.Fatal("missing expected immutable spec must fail before create")
	}
}

func TestClassifyExistingObjectAdoptsExactProjectionDespiteRuntimeMetadata(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	existing := expected.DeepCopy()
	existing.SetUID(types.UID("uid-from-api-server"))
	existing.SetResourceVersion("12345")
	annotations := existing.GetAnnotations()
	annotations["volcano.sh/controller-note"] = "runtime-owned"
	existing.SetAnnotations(annotations)
	labels := existing.GetLabels()
	labels["volcano.sh/runtime"] = "observed"
	existing.SetLabels(labels)
	existing.Object["status"] = map[string]any{"state": map[string]any{"phase": "Running"}}

	action, err := classifyExistingObject(expected, existing)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectAdopt {
		t.Fatalf("action=%q, want %q", action, existingObjectAdopt)
	}
}

func TestClassifyExistingObjectRejectsOwnershipAndGenerationConflicts(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{
			name: "provider",
			mutate: func(obj *unstructured.Unstructured) {
				annotations := obj.GetAnnotations()
				annotations[providerAnnotation] = "kueue"
				obj.SetAnnotations(annotations)
			},
		},
		{
			name: "generation",
			mutate: func(obj *unstructured.Unstructured) {
				annotations := obj.GetAnnotations()
				annotations[generationAnnotation] = "6"
				obj.SetAnnotations(annotations)
			},
		},
		{
			name: "workload owner",
			mutate: func(obj *unstructured.Unstructured) {
				annotations := obj.GetAnnotations()
				annotations[workloadIDAnnotation] = "another-workload"
				obj.SetAnnotations(annotations)
			},
		},
		{
			name: "pool owner",
			mutate: func(obj *unstructured.Unstructured) {
				annotations := obj.GetAnnotations()
				annotations[poolIDAnnotation] = "another-pool"
				obj.SetAnnotations(annotations)
			},
		},
		{
			name: "provider label",
			mutate: func(obj *unstructured.Unstructured) {
				labels := obj.GetLabels()
				labels["ai.compute/workload"] = "job-another"
				obj.SetLabels(labels)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := expected.DeepCopy()
			tt.mutate(existing)
			if _, err := classifyExistingObject(expected, existing); err == nil ||
				!strings.Contains(err.Error(), "provider object conflict") {
				t.Fatalf("conflict was not rejected: %v", err)
			}
		})
	}
}

func TestClassifyExistingObjectRejectsSameGenerationImmutableSpecDrift(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{
			name: "image",
			mutate: func(obj *unstructured.Unstructured) {
				tasks, _, _ := unstructured.NestedSlice(obj.Object, "spec", "tasks")
				task := tasks[0].(map[string]any)
				template := task["template"].(map[string]any)
				spec := template["spec"].(map[string]any)
				containers := spec["containers"].([]any)
				container := containers[0].(map[string]any)
				container["image"] = "example/other:stable"
				_ = unstructured.SetNestedSlice(obj.Object, tasks, "spec", "tasks")
			},
		},
		{
			name: "queue",
			mutate: func(obj *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(obj.Object, "vq-other", "spec", "queue")
			},
		},
		{
			name: "gpu quantity",
			mutate: func(obj *unstructured.Unstructured) {
				tasks, _, _ := unstructured.NestedSlice(obj.Object, "spec", "tasks")
				task := tasks[0].(map[string]any)
				template := task["template"].(map[string]any)
				spec := template["spec"].(map[string]any)
				containers := spec["containers"].([]any)
				container := containers[0].(map[string]any)
				resources := container["resources"].(map[string]any)
				requests := resources["requests"].(map[string]any)
				requests["nvidia.com/gpu"] = "3"
				_ = unstructured.SetNestedSlice(obj.Object, tasks, "spec", "tasks")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := expected.DeepCopy()
			tt.mutate(existing)
			if _, err := classifyExistingObject(expected, existing); err == nil ||
				!strings.Contains(err.Error(), "immutable provider projection differs") {
				t.Fatalf("same-generation immutable drift was not rejected: %v", err)
			}
		})
	}
}

func TestClassifyExistingQueueUsesSameCreateOrAdoptBoundary(t *testing.T) {
	expected, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	existing := expected.DeepCopy()
	action, err := classifyExistingObject(expected, existing)
	if err != nil || action != existingObjectAdopt {
		t.Fatalf("queue adoption failed: action=%q err=%v", action, err)
	}

	capability, _, _ := unstructured.NestedStringMap(existing.Object, "spec", "capability")
	capability["nvidia.com/gpu"] = "4"
	if err := unstructured.SetNestedStringMap(existing.Object, capability, "spec", "capability"); err != nil {
		t.Fatal(err)
	}
	if _, err := classifyExistingObject(expected, existing); err == nil {
		t.Fatal("queue capacity drift must not be adopted at the same generation")
	}
}

func TestClassifyExistingObjectAllowsVolcanoAndAPIServerDefaultMapFields(t *testing.T) {
	queue, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	existingQueue := queue.DeepCopy()
	queueSpec, _, _ := unstructured.NestedMap(existingQueue.Object, "spec")
	queueSpec["parent"] = "root"
	queueSpec["reclaimable"] = true
	queueSpec["dequeueStrategy"] = "traverse"
	queueSpec["weight"] = int64(1)
	if err := unstructured.SetNestedMap(existingQueue.Object, queueSpec, "spec"); err != nil {
		t.Fatal(err)
	}
	existingQueue.Object["status"] = map[string]any{"state": "Open"}
	if action, err := classifyExistingObject(queue, existingQueue); err != nil || action != existingObjectAdopt {
		t.Fatalf("defaulted Queue should be adoptable: action=%q err=%v", action, err)
	}

	job, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	existingJob := job.DeepCopy()
	jobSpec, _, _ := unstructured.NestedMap(existingJob.Object, "spec")
	jobSpec["maxRetry"] = int64(3)
	if err := unstructured.SetNestedMap(existingJob.Object, jobSpec, "spec"); err != nil {
		t.Fatal(err)
	}
	tasks, _, _ := unstructured.NestedSlice(existingJob.Object, "spec", "tasks")
	task := tasks[0].(map[string]any)
	template := task["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	podSpec["dnsPolicy"] = "ClusterFirst"
	podSpec["terminationGracePeriodSeconds"] = int64(30)
	if err := unstructured.SetNestedSlice(existingJob.Object, tasks, "spec", "tasks"); err != nil {
		t.Fatal(err)
	}
	if action, err := classifyExistingObject(job, existingJob); err != nil || action != existingObjectAdopt {
		t.Fatalf("defaulted VolcanoJob should be adoptable: action=%q err=%v", action, err)
	}
}

func TestProjectionSubsetMatchKeepsScalarTypesStrict(t *testing.T) {
	queue, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	existing := queue.DeepCopy()
	capability, _, _ := unstructured.NestedMap(existing.Object, "spec", "capability")
	capability["nvidia.com/gpu"] = int64(8)
	if err := unstructured.SetNestedMap(existing.Object, capability, "spec", "capability"); err != nil {
		t.Fatal(err)
	}
	if _, err := classifyExistingObject(queue, existing); err == nil {
		t.Fatal("string quantity and integer quantity must not compare equal")
	}
}

func TestClassifyExistingObjectRejectsUnreviewedSpecDefaultsAndInjectedBehavior(t *testing.T) {
	queue, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	job, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		base   *unstructured.Unstructured
		mutate func(*unstructured.Unstructured)
	}{
		{
			name: "queue extra capability",
			base: queue,
			mutate: func(obj *unstructured.Unstructured) {
				spec := obj.Object["spec"].(map[string]any)
				spec["capability"].(map[string]any)["example.com/other-gpu"] = "99"
			},
		},
		{
			name: "queue unreviewed scheduling field",
			base: queue,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["affinity"] = map[string]any{"unexpected": true}
			},
		},
		{
			name: "queue changed default weight",
			base: queue,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["weight"] = int64(999)
			},
		},
		{
			name: "queue false reclaimable is not the pinned Volcano default",
			base: queue,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["reclaimable"] = false
			},
		},
		{
			name: "queue alternate dequeue strategy",
			base: queue,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["dequeueStrategy"] = "fifo"
			},
		},
		{
			name: "queue non-scalar weight must not panic",
			base: queue,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["weight"] = map[string]any{"value": 1}
			},
		},
		{
			name: "job injected hostNetwork",
			base: job,
			mutate: func(obj *unstructured.Unstructured) {
				tasks := obj.Object["spec"].(map[string]any)["tasks"].([]any)
				podSpec := tasks[0].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
				podSpec["hostNetwork"] = true
			},
		},
		{
			name: "job injected node selector",
			base: job,
			mutate: func(obj *unstructured.Unstructured) {
				tasks := obj.Object["spec"].(map[string]any)["tasks"].([]any)
				podSpec := tasks[0].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
				podSpec["nodeSelector"] = map[string]any{"node": "stolen"}
			},
		},
		{
			name: "job added container resource request",
			base: job,
			mutate: func(obj *unstructured.Unstructured) {
				tasks := obj.Object["spec"].(map[string]any)["tasks"].([]any)
				podSpec := tasks[0].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
				container := podSpec["containers"].([]any)[0].(map[string]any)
				container["resources"].(map[string]any)["requests"].(map[string]any)["example.com/gpu"] = "2"
			},
		},
		{
			name: "job changed retry default",
			base: job,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["maxRetry"] = int64(30)
			},
		},
		{
			name: "job unreviewed extra retry policy",
			base: job,
			mutate: func(obj *unstructured.Unstructured) {
				obj.Object["spec"].(map[string]any)["ttlSecondsAfterFinished"] = int64(0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := tt.base.DeepCopy()
			tt.mutate(existing)
			if action, err := classifyExistingObject(tt.base, existing); err == nil {
				t.Fatalf("unreviewed spec drift was adopted: action=%q", action)
			}
		})
	}
}
