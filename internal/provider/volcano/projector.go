package volcano

import (
	"crypto/sha256"
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
	if in.Provider != ProviderName {
		return nil, fmt.Errorf("Volcano workload projection requires provider %q, got %q", ProviderName, in.Provider)
	}
	if in.Generation <= 0 {
		return nil, fmt.Errorf("Volcano workload generation must be positive")
	}
	if in.WorkloadID == "" || in.PoolID == "" || in.ClusterID == "" || in.Namespace == "" {
		return nil, fmt.Errorf("workload, pool, cluster and namespace are required")
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
	if in.Provider != ProviderName {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("Volcano pool projection requires provider %q, got %q", ProviderName, in.Provider)
	}
	if in.Generation <= 0 {
		return domain.AcceleratorRequest{}, domain.AcceleratorBinding{}, fmt.Errorf("Volcano pool generation must be positive")
	}
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

// resourceName binds the original platform ID to the provider object name.
// Normalizing only case/punctuation is unsafe: distinct IDs (e.g. a_b and
// a.b) would otherwise collide and could be mistaken for lost-ACK replay.
// The full original ID remains in the ownership annotation for adoption checks.
func resourceName(prefix, value string) string {
	var slug strings.Builder
	pendingSeparator := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingSeparator && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(r)
			pendingSeparator = false
		} else {
			pendingSeparator = true
		}
	}
	clean := slug.String()
	if clean == "" {
		clean = "id"
	}
	// 16 hex characters (64 bits) keep normalized/truncated names distinct
	// without storing the attempt or lease identity in a Kubernetes object name.
	sum := sha256.Sum256([]byte(value))
	const suffixLength = 16
	maxSlug := 63 - len(prefix) - 2 - suffixLength
	if maxSlug < 1 {
		panic("Volcano resource prefix exceeds Kubernetes DNS-label limit")
	}
	if len(clean) > maxSlug {
		clean = strings.Trim(clean[:maxSlug], "-")
		if clean == "" {
			clean = "id"
		}
	}
	return fmt.Sprintf("%s-%s-%x", prefix, clean, sum[:8])
}

func stringSliceAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}
