package cluster

import (
	"context"
	"fmt"
	"sort"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var clusterQueueGVR = schema.GroupVersionResource{
	Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "clusterqueues",
}

func (c *Clients) Discover(ctx context.Context) (domain.ClusterCapabilities, error) {
	if c == nil || c.Core == nil || c.Dynamic == nil {
		return domain.ClusterCapabilities{}, fmt.Errorf("cluster clients are required")
	}

	capabilities := domain.ClusterCapabilities{}

	if _, err := c.Dynamic.Resource(clusterQueueGVR).List(ctx, metav1.ListOptions{Limit: 1}); err == nil {
		capabilities.Kueue = true
	}

	nodes, err := c.Core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.ClusterCapabilities{}, fmt.Errorf("list nodes: %w", err)
	}

	classes := map[string]struct{}{}
	for _, node := range nodes.Items {
		if class := node.Labels["ai.compute/accelerator-class"]; class != "" {
			classes[class] = struct{}{}
		}
	}
	for class := range classes {
		capabilities.Accelerators = append(capabilities.Accelerators, class)
	}
	sort.Strings(capabilities.Accelerators)

	return capabilities, nil
}
