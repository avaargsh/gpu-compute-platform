package volcano

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestProjectPoolUsesPortableClassAndExtendedResourceWithoutKueueFlavor(t *testing.T) {
	obj, err := ProjectPool(baseprovider.PoolProjection{
		Provider:   ProviderName,
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
	if obj.GetName() != queueName("Pool_H100.V1") {
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
		Provider:   ProviderName,
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
	if obj.GetName() != resourceName("job", "Train_One.V1") || obj.GetNamespace() != "project-1" {
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
	if scheduler != "volcano" || queue != queueName("Pool_H100.V1") || minAvailable != 1 {
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
		Provider:   ProviderName,
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
				Provider:   ProviderName,
				WorkloadID: "train-1",
				PoolID:     "pool-a",
				ClusterID:  "cluster-a",
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

func TestVolcanoNamesCannotAliasDistinctPlatformIDs(t *testing.T) {
	for _, pair := range [][2]string{
		{"Pool_H100.V1", "pool-h100-v1"},
		{"foo_bar", "foo.bar"},
		{"pool-ABC", "pool-abc"},
		{"x/y", "x:y"},
	} {
		a, b := resourceName("vq", pair[0]), resourceName("vq", pair[1])
		if a == b {
			t.Fatalf("distinct IDs %q and %q collide at name %q", pair[0], pair[1], a)
		}
		if a != resourceName("vq", pair[0]) || b != resourceName("vq", pair[1]) {
			t.Fatal("provider resource naming must be deterministic across replay")
		}
	}

	dnsLabel := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	for _, id := range []string{
		"simple",
		"LONG_ID_WITH_MANY_PARTS." + strings.Repeat("Z", 190),
		"unicode-日本語",
		"////:::",
	} {
		for _, prefix := range []string{"vq", "job"} {
			got := resourceName(prefix, id)
			if len(got) > 63 || !dnsLabel.MatchString(got) {
				t.Fatalf("unsafe provider name for %q: %q", id, got)
			}
		}
	}
}

func TestVolcanoProjectionRequiresFrozenProviderAndPositiveGeneration(t *testing.T) {
	pool := baseprovider.PoolProjection{
		Provider:   ProviderName,
		PoolID:     "pool-one",
		ClusterID:  "cluster-a",
		Namespace:  "project-a",
		Generation: 1,
		Accelerators: []domain.AcceleratorRequest{{
			Class: "h100-80g", Quota: 1,
		}},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class: "h100-80g", ResourceName: "nvidia.com/gpu",
		}},
	}
	workload := baseprovider.WorkloadProjection{
		Provider:   ProviderName,
		WorkloadID: "train-one",
		PoolID:     "pool-one",
		ClusterID:  "cluster-a",
		Namespace:  "project-a",
		Generation: 1,
		Image:      "train:stable",
		Accelerator: domain.AcceleratorRequest{
			Class: "h100-80g", Quota: 1,
		},
		AcceleratorBinding: domain.AcceleratorBinding{
			Class: "h100-80g", ResourceName: "nvidia.com/gpu",
		},
	}
	if _, err := ProjectPool(pool); err != nil {
		t.Fatalf("valid Volcano pool fixture: %v", err)
	}
	if _, err := ProjectWorkload(workload); err != nil {
		t.Fatalf("valid Volcano workload fixture: %v", err)
	}
	for _, name := range []string{"", "kueue", "volcano-other"} {
		t.Run("provider="+name, func(t *testing.T) {
			mutatedPool, mutatedWorkload := pool, workload
			mutatedPool.Provider = name
			mutatedWorkload.Provider = name
			if _, err := ProjectPool(mutatedPool); err == nil {
				t.Fatal("Volcano pool projector must reject non-Volcano provider")
			}
			if _, err := ProjectWorkload(mutatedWorkload); err == nil {
				t.Fatal("Volcano workload projector must reject non-Volcano provider")
			}
		})
	}
	for _, generation := range []int64{0, -1} {
		mutatedPool, mutatedWorkload := pool, workload
		mutatedPool.Generation = generation
		mutatedWorkload.Generation = generation
		if _, err := ProjectPool(mutatedPool); err == nil {
			t.Fatalf("Volcano pool accepted generation %d", generation)
		}
		if _, err := ProjectWorkload(mutatedWorkload); err == nil {
			t.Fatalf("Volcano workload accepted generation %d", generation)
		}
	}
}
