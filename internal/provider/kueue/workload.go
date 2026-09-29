package kueue

import (
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type Job struct {
	Name        string
	Namespace   string
	QueueName   string
	Image       string
	Command     []string
	Resources   map[string]int64
	Labels      map[string]string
	Annotations map[string]string
	DRA         *DRARequest
}

type DRARequest struct {
	ClaimName       string
	DeviceClassName string
	Count           int64
}

func ProjectWorkload(in baseprovider.WorkloadProjection) (Job, error) {
	if in.WorkloadID == "" || in.PoolID == "" || in.Namespace == "" {
		return Job{}, fmt.Errorf("workload, pool and namespace are required")
	}
	if in.Image == "" {
		return Job{}, fmt.Errorf("workload image is required")
	}
	if in.Accelerator.Class == "" || in.Accelerator.Quota <= 0 {
		return Job{}, fmt.Errorf("accelerator class and positive count are required")
	}
	binding := in.AcceleratorBinding
	if binding.Class == "" || binding.ResourceName == "" || binding.Flavor == "" {
		return Job{}, fmt.Errorf("accelerator binding is required")
	}
	if binding.AllocationMode != "" && binding.AllocationMode != domain.AcceleratorAllocationExtendedResource && binding.AllocationMode != domain.AcceleratorAllocationDRA {
		return Job{}, fmt.Errorf("unsupported accelerator allocation mode %q", binding.AllocationMode)
	}
	if binding.AllocationMode == domain.AcceleratorAllocationDRA {
		if binding.DRA == nil || binding.DRA.DeviceClassName == "" {
			return Job{}, fmt.Errorf("DRA allocation requires device class name")
		}
		return Job{
			Name: resourceName("job", string(in.WorkloadID)), Namespace: in.Namespace,
			QueueName: resourceName("lq", string(in.PoolID)), Image: in.Image,
			Command: append([]string(nil), in.Command...),
			Labels: map[string]string{"kueue.x-k8s.io/queue-name": resourceName("lq", string(in.PoolID))},
			Annotations: map[string]string{"ai.compute/accelerator-class": in.Accelerator.Class, "ai.compute/accelerator-flavor": binding.Flavor},
			DRA: &DRARequest{ClaimName: resourceName("accelerator", string(in.WorkloadID)), DeviceClassName: binding.DRA.DeviceClassName, Count: in.Accelerator.Quota},
		}, nil
	}
	if binding.Partition != nil && (binding.Partition.Kind != domain.AcceleratorPartitionMIG || binding.Partition.Profile == "") {
		return Job{}, fmt.Errorf("unsupported accelerator partition: %#v", binding.Partition)
	}
	if binding.Class != in.Accelerator.Class {
		return Job{}, fmt.Errorf("accelerator binding class mismatch: %s", in.Accelerator.Class)
	}

	return Job{
		Name:      resourceName("job", string(in.WorkloadID)),
		Namespace: in.Namespace,
		QueueName: resourceName("lq", string(in.PoolID)),
		Image:     in.Image,
		Command:   append([]string(nil), in.Command...),
		Resources: map[string]int64{
			binding.ResourceName: in.Accelerator.Quota,
		},
		Labels: map[string]string{
			"kueue.x-k8s.io/queue-name": resourceName("lq", string(in.PoolID)),
		},
		Annotations: map[string]string{
			"ai.compute/accelerator-class":  in.Accelerator.Class,
			"ai.compute/accelerator-flavor": binding.Flavor,
		},
	}, nil
}
