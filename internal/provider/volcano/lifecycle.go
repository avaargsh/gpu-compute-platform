package volcano

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type projectedObjectLifecycleClient interface {
	projectedObjectClient
	Delete(context.Context, *unstructured.Unstructured) error
}

type projectedObjectObservation struct {
	Exists bool
	UID    types.UID
	Phase  string
}

// observeProjectedObject is an independent read boundary after provider
// mutation. It never treats deterministic name alone as ownership proof.
//
// Existing objects must still satisfy the same strict provider/generation/spec
// classifier used by create-or-adopt. A missing object is a valid observation;
// callers decide whether that means "not yet created" or deletion convergence.
func observeProjectedObject(
	ctx context.Context,
	client projectedObjectClient,
	expected *unstructured.Unstructured,
) (projectedObjectObservation, error) {
	if client == nil {
		return projectedObjectObservation{}, fmt.Errorf("Volcano projected-object client is required")
	}
	if _, err := classifyExistingObject(expected, nil); err != nil {
		return projectedObjectObservation{}, err
	}

	current, err := client.Get(ctx, expected)
	if apierrors.IsNotFound(err) {
		return projectedObjectObservation{Exists: false}, nil
	}
	if err != nil {
		return projectedObjectObservation{}, fmt.Errorf("observe deterministic Volcano object: %w", err)
	}

	if _, err := classifyExistingObject(expected, current); err != nil {
		return projectedObjectObservation{}, err
	}

	phase := ""
	if current.GetKind() == "Job" {
		var found bool
		phase, found, err = unstructured.NestedString(current.Object, "status", "state", "phase")
		if err != nil {
			return projectedObjectObservation{}, fmt.Errorf("read Volcano Job status.state.phase: %w", err)
		}
		if !found {
			phase = ""
		}
	}

	return projectedObjectObservation{
		Exists: true,
		UID:    current.GetUID(),
		Phase:  phase,
	}, nil
}

// deleteProjectedObject implements observe-until-gone deletion.
//
// The object is first re-read and ownership-classified before DELETE. A
// deterministic-name collision or generation/spec drift therefore fails closed
// instead of deleting a foreign/stale object.
//
// DELETE success is not treated as final convergence. The function performs a
// fresh GET and reports Gone only after Kubernetes returns NotFound. If DELETE
// commits remotely but its acknowledgement is lost, the error is preserved; the
// next reconciliation observes NotFound and safely converges.
func deleteProjectedObject(
	ctx context.Context,
	client projectedObjectLifecycleClient,
	expected *unstructured.Unstructured,
) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("Volcano projected-object lifecycle client is required")
	}
	if _, err := classifyExistingObject(expected, nil); err != nil {
		return false, err
	}

	current, err := client.Get(ctx, expected)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get Volcano object before delete: %w", err)
	}
	if _, err := classifyExistingObject(expected, current); err != nil {
		return false, err
	}

	if err := client.Delete(ctx, expected); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("delete deterministic Volcano object: %w", err)
	}

	current, err = client.Get(ctx, expected)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("observe Volcano object after delete: %w", err)
	}
	if _, err := classifyExistingObject(expected, current); err != nil {
		return false, err
	}
	return false, nil
}
