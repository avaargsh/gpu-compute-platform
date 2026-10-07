package volcano

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type deletionHarness struct {
	deleteErr error
	gone      bool
	goneErr   error
	deletes   int
}

func (h *deletionHarness) Delete(context.Context, *unstructured.Unstructured) error {
	h.deletes++
	return h.deleteErr
}
func (h *deletionHarness) Gone(context.Context, *unstructured.Unstructured) (bool, error) {
	return h.gone, h.goneErr
}

func TestDeleteProjectedObjectWaitsUntilGone(t *testing.T) {
	h := &deletionHarness{gone: false}
	gone, err := deleteProjectedObject(context.Background(), h, &unstructured.Unstructured{})
	if err != nil || gone {
		t.Fatalf("gone=%v err=%v, want pending cleanup", gone, err)
	}
}

func TestDeleteProjectedObjectTreatsNotFoundAsReplaySafe(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "batch.volcano.sh", Resource: "jobs"}, "job-a")
	h := &deletionHarness{deleteErr: notFound, gone: true}
	gone, err := deleteProjectedObject(context.Background(), h, &unstructured.Unstructured{})
	if err != nil || !gone {
		t.Fatalf("gone=%v err=%v, want terminal gone", gone, err)
	}
}

func TestDeleteProjectedObjectLostAckDoesNotClaimGone(t *testing.T) {
	h := &deletionHarness{deleteErr: errors.New("transport timeout after remote delete")}
	gone, err := deleteProjectedObject(context.Background(), h, &unstructured.Unstructured{})
	if err == nil || gone {
		t.Fatalf("gone=%v err=%v, want ambiguous failure", gone, err)
	}
}

func TestDeleteProjectedObjectObservationErrorFailsClosed(t *testing.T) {
	h := &deletionHarness{goneErr: errors.New("observe unavailable")}
	gone, err := deleteProjectedObject(context.Background(), h, &unstructured.Unstructured{})
	if err == nil || gone {
		t.Fatalf("gone=%v err=%v, want observation failure", gone, err)
	}
}
