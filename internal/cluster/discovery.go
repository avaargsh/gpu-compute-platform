package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

var clusterQueueGVR = schema.GroupVersionResource{
	Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "clusterqueues",
}

var draGroupVersions = []string{
	"resource.k8s.io/v1",
	"resource.k8s.io/v1beta1",
}

func (c *Clients) Discover(ctx context.Context) (domain.ClusterCapabilities, error) {
	if c == nil || c.Core == nil || c.Dynamic == nil {
		return domain.ClusterCapabilities{}, fmt.Errorf("cluster clients are required")
	}

	capabilities := domain.ClusterCapabilities{}

	if _, err := c.Dynamic.Resource(clusterQueueGVR).List(ctx, metav1.ListOptions{Limit: 1}); err == nil {
		capabilities.Kueue = true
		capabilities.Schedulers = append(capabilities.Schedulers, domain.SchedulerCapability{
			Name:    "kueue",
			Version: c.discoverKueueVersion(ctx),
		})
	}

	if supported, apiVersion := discoverDRA(c.Core.Discovery()); supported {
		capabilities.DRAAPIAvailable = true
		capabilities.DRAAPIVersion = apiVersion
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
	sort.Slice(capabilities.Schedulers, func(i, j int) bool {
		if capabilities.Schedulers[i].Name == capabilities.Schedulers[j].Name {
			return capabilities.Schedulers[i].Version < capabilities.Schedulers[j].Version
		}
		return capabilities.Schedulers[i].Name < capabilities.Schedulers[j].Name
	})

	return capabilities, nil
}

func (c *Clients) discoverKueueVersion(ctx context.Context) string {
	deployments, err := c.Core.AppsV1().Deployments(metav1.NamespaceAll).List(
		ctx,
		metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=kueue"},
	)
	if err != nil {
		return ""
	}

	for _, deployment := range deployments.Items {
		if version := strings.TrimSpace(deployment.Labels["app.kubernetes.io/version"]); version != "" {
			return version
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name != "manager" && !strings.Contains(container.Image, "/kueue") {
				continue
			}
			if version := imageTag(container.Image); version != "" {
				return version
			}
		}
	}

	return ""
}

func imageTag(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash || colon == len(image)-1 {
		return ""
	}
	return image[colon+1:]
}

func discoverDRA(client discovery.DiscoveryInterface) (bool, string) {
	if client == nil {
		return false, ""
	}
	for _, groupVersion := range draGroupVersions {
		resources, err := client.ServerResourcesForGroupVersion(groupVersion)
		if err != nil || resources == nil {
			continue
		}
		for _, resource := range resources.APIResources {
			if resource.Name == "resourceclaims" {
				return true, groupVersion
			}
		}
	}
	return false, ""
}
