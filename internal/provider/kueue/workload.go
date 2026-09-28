package kueue

import (
	"fmt"

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

	return Job{
		Name:      resourceName("job", string(in.WorkloadID)),
		Namespace: in.Namespace,
		QueueName: resourceName("lq", string(in.PoolID)),
		Image:     in.Image,
		Command:   append([]string(nil), in.Command...),
		Resources: map[string]int64{
			gpuResourceName: in.Accelerator.Quota,
		},
		Labels: map[string]string{
			"kueue.x-k8s.io/queue-name": resourceName("lq", string(in.PoolID)),
		},
		Annotations: map[string]string{
			"ai.compute/accelerator-class": in.Accelerator.Class,
		},
	}, nil
}
