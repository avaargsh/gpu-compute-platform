package kueue

import (
	"fmt"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func ProjectPool(in provider.PoolProjection) (PoolResources, error) {
	if in.PoolID == "" || in.ClusterID == "" || in.Namespace == "" {
		return PoolResources{}, fmt.Errorf("pool, cluster and namespace are required")
	}

	out := PoolResources{
		ClusterQueue: ClusterQueue{Name: resourceName("cq", string(in.PoolID))},
		LocalQueue: LocalQueue{
			Name:         resourceName("lq", string(in.PoolID)),
			Namespace:    in.Namespace,
			ClusterQueue: resourceName("cq", string(in.PoolID)),
		},
	}

	bindings := make(map[string]domain.AcceleratorBinding, len(in.AcceleratorBindings))
	for _, binding := range in.AcceleratorBindings {
		if binding.AllocationMode == "" {
			binding.AllocationMode = domain.AcceleratorAllocationExtendedResource
		}
		if binding.AllocationMode != domain.AcceleratorAllocationExtendedResource {
			return PoolResources{}, fmt.Errorf("accelerator allocation mode %q is not supported by the Kueue extended-resource provider", binding.AllocationMode)
		}
		if binding.Partition != nil {
			if binding.Partition.Kind != domain.AcceleratorPartitionMIG || binding.Partition.Profile == "" {
				return PoolResources{}, fmt.Errorf("unsupported accelerator partition: %#v", binding.Partition)
			}
		}
		if binding.Class == "" || binding.ResourceName == "" || binding.Flavor == "" {
			return PoolResources{}, fmt.Errorf("accelerator binding class, resource name and flavor are required")
		}
		if _, exists := bindings[binding.Class]; exists {
			return PoolResources{}, fmt.Errorf("duplicate accelerator binding: %s", binding.Class)
		}
		bindings[binding.Class] = binding
	}

	for _, accelerator := range in.Accelerators {
		if accelerator.Class == "" || accelerator.Quota <= 0 {
			return PoolResources{}, fmt.Errorf("accelerator class and positive quota are required")
		}
		binding, ok := bindings[accelerator.Class]
		if !ok {
			return PoolResources{}, fmt.Errorf("accelerator binding not found: %s", accelerator.Class)
		}
		out.Flavors = append(out.Flavors, ResourceFlavor{
			Name:         binding.Flavor,
			ResourceName: binding.ResourceName,
			NodeLabels:   cloneLabels(binding.NodeLabels),
		})
		out.ClusterQueue.Quotas = append(out.ClusterQueue.Quotas, ResourceQuota{
			Flavor:   binding.Flavor,
			Resource: binding.ResourceName,
			Nominal:  accelerator.Quota,
		})
	}

	return out, nil
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func resourceName(prefix, value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	return prefix + "-" + value
}
