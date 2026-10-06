package volcano

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	ProviderName = "volcano"

	generationAnnotation       = "ai.compute/generation"
	providerAnnotation         = "ai.compute/provider"
	poolIDAnnotation           = "ai.compute/pool-id"
	workloadIDAnnotation       = "ai.compute/workload-id"
	acceleratorClassAnnotation = "ai.compute/accelerator-class"
)

func ProjectPool(in baseprovider.PoolProjection) (*unstructured.Unstructured, error) {
	accelerator, binding, err := validatePoolProjection(in)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "scheduling.volcano.sh/v1beta1",
		"kind":       "Queue",
		"metadata": map[string]any{
			"name": queueName(in.PoolID),
			"annotations": map[string]any{
				generationAnnotation:       strconv.FormatInt(in.Generation, 10),
				providerAnnotation:         ProviderName,
				poolIDAnnotation:           string(in.PoolID),
				acceleratorClassAnnotation: accelerator.Class,
			},
		},
		"spec": map[string]any{
			"capability": map[string]any{
				binding.ResourceName: strconv.FormatInt(accelerator.Quota, 10),
			},
		},
	}}, nil
}

func ProjectWorkload(in baseprovider.WorkloadProjection) (*unstructured.Unstructured, error) {
	if in.WorkloadID == "" || in.PoolID == "" || in.Namespace == "" {
		return nil, fmt.Errorf("workload, pool and namespace are required")
	}
	if in.Image == "" {
		return nil, fmt.Errorf("workload image is required")
	}
	if in.Accelerator.Class == "" || in.Accelerator.Quota <= 0 {
		return nil, fmt.Errorf("accelerator class and positive count are required")
	}

	binding, err := validateBinding(in.AcceleratorBinding, in.Accelerator.Class)
	if err != nil {
		return nil, err
	}

	quantity := strconv.FormatInt(in.Accelerator.Quota, 10)
	annotations := map[string]any{
		generationAnnotation:       strconv.FormatInt(in.Generation, 10),
		providerAnnotation:         ProviderName,
		poolIDAnnotation:           string(in.PoolID),
		workloadIDAnnotation:       string(in.WorkloadID),
		acceleratorClassAnnotation: in.Accelerator.Class,
	}
	labels := map[string]any{
		"ai.compute/workload": resourceName("job", string(in.WorkloadID)),
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch.volcano.sh/v1alpha1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":        resourceName("job", string(in.WorkloadID)),
			"namespace":   in.Namespace,
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": map[string]any{
			"schedulerName": "volcano",
			"queue":         queueName(in.PoolID),
			"minAvailable":  int64(1),
			"tasks": []any{
				map[string]any{
					"name":     "workload",
					"replicas": int64(1),
					"template": map[string]any{
						"metadata": map[string]any{
							"labels": labels,
						},
						"spec": map[string]any{
							"restartPolicy": "Never",
							"containers": []any{
								map[string]any{
									"name":    "workload",
									"image":   in.Image,
									"command": stringSliceAny(in.Command),
									"resources": map[string]any{
										"requests": map[string]any{binding.ResourceName: quantity},
										"limits":   map[string]any{binding.ResourceName: quantity},
									},
								},
							},
						},
					},
				},
			},
		},
	}}, nil
}

func validatePoolProjection(in baseprovider.PoolProjection) (domain.AcceleratorRequest, domain.AcceleratorBinding, error) {
	if in.PoolID == "" || in.ClusterID == "" || in.Namespace == "" {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("pool, cluster and namespace are required")
	}
	if len(in.Accelerators) != 1 {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("Volcano contract slice requires exactly one accelerator class")
	}

	accelerator := in.Accelerators[0]
	if accelerator.Class == "" || accelerator.Quota <= 0 {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("accelerator class and positive quota are required")
	}

	var matched *domain.AcceleratorBinding
	for i := range in.AcceleratorBindings {
		if in.AcceleratorBindings[i].Class != accelerator.Class {
			continue
		}
		if matched != nil {
			return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("duplicate accelerator binding: %s", accelerator.Class)
		}
		candidate := in.AcceleratorBindings[i]
		matched = &candidate
	}
	if matched == nil {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("accelerator binding not found: %s", accelerator.Class)
	}

	binding, err := validateBinding(*matched, accelerator.Class)
	if err != nil {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, err
	}
	return accelerator, binding, nil
}

func validateBinding(binding domain.AcceleratorBinding, acceleratorClass string) (domain.AcceleratorBinding, error) {
	if binding.Class == "" {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator binding is required")
	}
	if binding.Class != acceleratorClass {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator binding class mismatch: %s", acceleratorClass)
	}

	mode := binding.AllocationMode
	if mode == "" {
		mode = domain.AcceleratorAllocationExtendedResource
	}
	if mode != domain.AcceleratorAllocationExtendedResource {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator allocation mode %q is outside the Volcano whole-GPU contract", mode)
	}
	if binding.ResourceName == "" {
		return domain.AcceleratorBinding{}, fmt.Errorf("extended-resource allocation requires resource name")
	}
	if binding.Partition != nil {
		return domain.AcceleratorBinding{}, fmt.Errorf("accelerator partitions are outside the Volcano whole-GPU contract")
	}
	if binding.DRA != nil {
		return domain.AcceleratorBinding{}, fmt.Errorf("DRA allocation is outside the Volcano whole-GPU contract")
	}
	if len(binding.NodeLabels) != 0 {
		return domain.AcceleratorBinding{}, fmt.Errorf("topology/provider node labels are outside the initial Volcano contract")
	}

	binding.AllocationMode = mode
	return binding, nil
}

func queueName(poolID domain.ID) string {
	return resourceName("vq", string(poolID))
}

func resourceName(prefix, value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	return prefix + "-" + value
}

func stringSliceAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}
