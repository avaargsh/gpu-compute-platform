package volcano

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type projectedObjectDeletionClient interface {
	Delete(context.Context, *unstructured.Unstructured) error
	Gone(context.Context, *unstructured.Unstructured) (bool, error)
}

// deleteProjectedObject is deliberately two-phase: request deletion, then
// re-observe until the deterministic provider object is gone. A lost delete ACK
// is safe because NotFound is terminal success and replay never blind-creates.
func deleteProjectedObject(ctx context.Context, client projectedObjectDeletionClient, expected *unstructured.Unstructured) (bool, error) {
	err := client.Delete(ctx, expected)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	return client.Gone(ctx, expected)
}
