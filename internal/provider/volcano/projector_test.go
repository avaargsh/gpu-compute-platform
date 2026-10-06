package volcano

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestProjectPoolUsesPortableClassAndExtendedResourceWithoutKueueFlavor(t *testing.T) {
	obj, err := ProjectPool(baseprovider.PoolProjection{
		PoolID:     "Pool_H100.V1",
		ProjectID:  "project-1",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 4,
		Accelerators: []domain.AcceleratorRequest{
			{Class: "h100-80g", Quota: 8},
		},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class:        "h100-80g",
			ResourceName: "nvidia.com/gpu",
			// Flavor is intentionally empty: Volcano projection must not depend
			// on the Kueue-specific compatibility field.
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if obj.GetAPIVersion() != "scheduling.volcano.sh/v1beta1" || obj.GetKind() != "Queue" {
		t.Fatalf("unexpected Volcano Queue GVK: %s %s", obj.GetAPIVersion(), obj.GetKind())
	}
	if obj.GetName() != "vq-pool-h100-v1" {
		t.Fatalf("queue name=%q, want deterministic normalized identity", obj.GetName())
	}

	annotations := obj.GetAnnotations()
	if annotations[providerAnnotation] != ProviderName ||
		annotations[generationAnnotation] != "4" ||
		annotations[poolIDAnnotation] != "Pool_H100.V1" ||
		annotations[acceleratorClassAnnotation] != "h100-80g" {
		t.Fatalf("unexpected ownership annotations: %#v", annotations)
	}

	capability, found, err := unstructured.NestedStringMap(obj.Object, "spec", "capability")
	if err != nil || !found {
		t.Fatalf("queue capability missing: found=%t err=%v object=%#v", found, err, obj.Object)
	}
	if capability["nvidia.com/gpu"] != "8" {
		t.Fatalf("unexpected queue capability: %#v", capability)
	}

	raw, err := json.Marshal(obj.Object)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "kueue") || strings.Contains(string(raw), "flavor") {
		t.Fatalf("Volcano projection leaked Kueue-specific fields: %s", raw)
	}
}

func TestProjectWorkloadBuildsDeterministicVolcanoJob(t *testing.T) {
	obj, err := ProjectWorkload(baseprovider.WorkloadProjection{
		WorkloadID: "Train_One.V1",
		ProjectID:  "project-1",
		PoolID:     "Pool_H100.V1",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 7,
		Image:      "example/train:latest",
		Command:    []string{"python", "train.py"},
		Accelerator: domain.AcceleratorRequest{
			Class: "h100-80g",
			Quota: 2,
		},
		AcceleratorBinding: domain.AcceleratorBinding{
			Class:        "h100-80g",
			ResourceName: "nvidia.com/gpu",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if obj.GetAPIVersion() != "batch.volcano.sh/v1alpha1" || obj.GetKind() != "Job" {
		t.Fatalf("unexpected VolcanoJob GVK: %s %s", obj.GetAPIVersion(), obj.GetKind())
	}
	if obj.GetName() != "job-train-one-v1" || obj.GetNamespace() != "project-1" {
		t.Fatalf("unexpected workload identity: %s/%s", obj.GetNamespace(), obj.GetName())
	}

	annotations := obj.GetAnnotations()
	if annotations[providerAnnotation] != ProviderName ||
		annotations[generationAnnotation] != "7" ||
		annotations[workloadIDAnnotation] != "Train_One.V1" ||
		annotations[poolIDAnnotation] != "Pool_H100.V1" {
		t.Fatalf("unexpected ownership annotations: %#v", annotations)
	}

	scheduler, _, _ := unstructured.NestedString(obj.Object, "spec", "schedulerName")
	queue, _, _ := unstructured.NestedString(obj.Object, "spec", "queue")
	minAvailable, _, _ := unstructured.NestedInt64(obj.Object, "spec", "minAvailable")
	if scheduler != "volcano" || queue != "vq-pool-h100-v1" || minAvailable != 1 {
		t.Fatalf("unexpected Volcano scheduling projection: scheduler=%q queue=%q min=%d", scheduler, queue, minAvailable)
	}

	tasks, found, err := unstructured.NestedSlice(obj.Object, "spec", "tasks")
	if err != nil || !found || len(tasks) != 1 {
		t.Fatalf("unexpected tasks: found=%t err=%v tasks=%#v", found, err, tasks)
	}
	task, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("task shape=%T, want map", tasks[0])
	}
	if task["name"] != "workload" || task["replicas"] != int64(1) {
		t.Fatalf("unexpected task identity: %#v", task)
	}

	template := task["template"].(map[string]any)
	spec := template["spec"].(map[string]any)
	containers := spec["containers"].([]any)
	container := containers[0].(map[string]any)
	resources := container["resources"].(map[string]any)
	requests := resources["requests"].(map[string]any)
	limits := resources["limits"].(map[string]any)
	if requests["nvidia.com/gpu"] != "2" || limits["nvidia.com/gpu"] != "2" {
		t.Fatalf("portable accelerator binding was not projected: requests=%#v limits=%#v", requests, limits)
	}

	raw, err := json.Marshal(obj.Object)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "kueue") || strings.Contains(string(raw), "flavor") {
		t.Fatalf("Volcano workload projection leaked Kueue-specific fields: %s", raw)
	}
}

func TestVolcanoProjectionFailsClosedOutsideInitialContract(t *testing.T) {
	base := baseprovider.PoolProjection{
		PoolID:     "pool-a",
		ClusterID:  "cluster-a",
		Namespace:  "project-a",
		Generation: 1,
		Accelerators: []domain.AcceleratorRequest{
			{Class: "h100-80g", Quota: 4},
		},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class:        "h100-80g",
			ResourceName: "nvidia.com/gpu",
		}},
	}

	tests := []struct {
		name   string
		mutate func(*baseprovider.PoolProjection)
	}{
		{
			name: "multiple accelerator classes",
			mutate: func(in *baseprovider.PoolProjection) {
				in.Accelerators = append(in.Accelerators, domain.AcceleratorRequest{Class: "a100-80g", Quota: 2})
			},
		},
		{
			name: "DRA",
			mutate: func(in *baseprovider.PoolProjection) {
				in.AcceleratorBindings[0].AllocationMode = domain.AcceleratorAllocationDRA
				in.AcceleratorBindings[0].DRA = &domain.DRAAllocation{DeviceClassName: "gpu.nvidia.com"}
			},
		},
		{
			name: "MIG partition",
			mutate: func(in *baseprovider.PoolProjection) {
				in.AcceleratorBindings[0].Partition = &domain.AcceleratorPartition{
					Kind: domain.AcceleratorPartitionMIG, Profile: "1g.10gb",
				}
			},
		},
		{
			name: "topology labels",
			mutate: func(in *baseprovider.PoolProjection) {
				in.AcceleratorBindings[0].NodeLabels = map[string]string{"topology.kubernetes.io/zone": "gpu-a"}
			},
		},
		{
			name: "missing binding",
			mutate: func(in *baseprovider.PoolProjection) {
				in.AcceleratorBindings = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			in.Accelerators = append([]domain.AcceleratorRequest(nil), base.Accelerators...)
			in.AcceleratorBindings = append([]domain.AcceleratorBinding(nil), base.AcceleratorBindings...)
			tt.mutate(&in)
			if _, err := ProjectPool(in); err == nil {
				t.Fatalf("%s unexpectedly projected", tt.name)
			}
		})
	}
}

func TestProjectWorkloadRejectsUnsupportedAllocationAndBindingMismatch(t *testing.T) {
	tests := []struct {
		name    string
		binding domain.AcceleratorBinding
	}{
		{
			name: "DRA",
			binding: domain.AcceleratorBinding{
				Class: "h100-80g", AllocationMode: domain.AcceleratorAllocationDRA,
				ResourceName: "nvidia.com/gpu", DRA: &domain.DRAAllocation{DeviceClassName: "gpu.nvidia.com"},
			},
		},
		{
			name: "MIG",
			binding: domain.AcceleratorBinding{
				Class: "h100-80g", ResourceName: "nvidia.com/mig-1g.10gb",
				Partition: &domain.AcceleratorPartition{Kind: domain.AcceleratorPartitionMIG, Profile: "1g.10gb"},
			},
		},
		{
			name: "class mismatch",
			binding: domain.AcceleratorBinding{
				Class: "a100-80g", ResourceName: "nvidia.com/gpu",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ProjectWorkload(baseprovider.WorkloadProjection{
				WorkloadID: "train-1",
				PoolID:     "pool-a",
				Namespace:  "project-a",
				Generation: 1,
				Image:      "example/train:latest",
				Accelerator: domain.AcceleratorRequest{
					Class: "h100-80g", Quota: 1,
				},
				AcceleratorBinding: tt.binding,
			})
			if err == nil {
				t.Fatalf("%s unexpectedly projected", tt.name)
			}
		})
	}
}
