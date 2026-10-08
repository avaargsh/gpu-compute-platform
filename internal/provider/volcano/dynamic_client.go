package volcano

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	volcanoQueueGVR = schema.GroupVersionResource{
		Group: "scheduling.volcano.sh", Version: "v1beta1", Resource: "queues",
	}
	volcanoJobGVR = schema.GroupVersionResource{
		Group: "batch.volcano.sh", Version: "v1alpha1", Resource: "jobs",
	}
)

type dynamicProjectedObjectClient struct {
	dynamic dynamic.Interface
}

func newDynamicProjectedObjectClient(client dynamic.Interface) (*dynamicProjectedObjectClient, error) {
	if client == nil {
		return nil, fmt.Errorf("Kubernetes dynamic client is required")
	}
	return &dynamicProjectedObjectClient{dynamic: client}, nil
}

func (c *dynamicProjectedObjectClient) Get(
	ctx context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	resource, err := c.resource(expected)
	if err != nil {
		return nil, err
	}
	return resource.Get(ctx, expected.GetName(), metav1.GetOptions{})
}

func (c *dynamicProjectedObjectClient) Create(
	ctx context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	resource, err := c.resource(expected)
	if err != nil {
		return nil, err
	}
	return resource.Create(ctx, expected.DeepCopy(), metav1.CreateOptions{})
}

func (c *dynamicProjectedObjectClient) Delete(
	ctx context.Context,
	observed *unstructured.Unstructured,
) error {
	resource, err := c.resource(observed)
	if err != nil {
		return err
	}
	// DELETE by deterministic name alone is unsafe after a separate ownership
	// GET. Require the same UID and resourceVersion the classifier observed.
	uid := observed.GetUID()
	rv := observed.GetResourceVersion()
	if uid == "" || rv == "" {
		return fmt.Errorf("Volcano DELETE requires observed UID and resourceVersion")
	}
	return resource.Delete(ctx, observed.GetName(), metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             &uid,
			ResourceVersion: &rv,
		},
	})
}

func (c *dynamicProjectedObjectClient) resource(
	expected *unstructured.Unstructured,
) (dynamic.ResourceInterface, error) {
	if c == nil || c.dynamic == nil {
		return nil, fmt.Errorf("Kubernetes dynamic client is required")
	}
	if expected == nil {
		return nil, fmt.Errorf("expected Volcano provider object is required")
	}

	switch {
	case expected.GetAPIVersion() == "scheduling.volcano.sh/v1beta1" &&
		expected.GetKind() == "Queue":
		if expected.GetNamespace() != "" {
			return nil, fmt.Errorf("Volcano Queue must be cluster-scoped")
		}
		return c.dynamic.Resource(volcanoQueueGVR), nil
	case expected.GetAPIVersion() == "batch.volcano.sh/v1alpha1" &&
		expected.GetKind() == "Job":
		if expected.GetNamespace() == "" {
			return nil, fmt.Errorf("Volcano Job namespace is required")
		}
		return c.dynamic.Resource(volcanoJobGVR).Namespace(expected.GetNamespace()), nil
	default:
		return nil, fmt.Errorf(
			"unsupported Volcano provider GVK %s %s",
			expected.GetAPIVersion(),
			expected.GetKind(),
		)
	}
}
