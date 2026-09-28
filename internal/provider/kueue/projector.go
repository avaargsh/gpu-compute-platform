package kueue

import (
	"fmt"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

const gpuResourceName = "nvidia.com/gpu"

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

	for _, accelerator := range in.Accelerators {
		if accelerator.Class == "" || accelerator.Quota <= 0 {
			return PoolResources{}, fmt.Errorf("accelerator class and positive quota are required")
		}

		flavor := resourceName("accel", accelerator.Class)
		out.Flavors = append(out.Flavors, ResourceFlavor{
			Name:         flavor,
			ResourceName: gpuResourceName,
			NodeLabels: map[string]string{
				"ai.compute/accelerator-class": accelerator.Class,
			},
		})
		out.ClusterQueue.Quotas = append(out.ClusterQueue.Quotas, ResourceQuota{
			Flavor:   flavor,
			Resource: gpuResourceName,
			Nominal:  accelerator.Quota,
		})
	}

	return out, nil
}

func resourceName(prefix, value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	return prefix + "-" + value
}
