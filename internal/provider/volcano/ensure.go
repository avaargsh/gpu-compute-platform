package volcano

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type projectedObjectClient interface {
	Get(context.Context, *unstructured.Unstructured) (*unstructured.Unstructured, error)
	Create(context.Context, *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

// ensureProjectedObject implements the provider-side at-least-once create
// boundary without owning any higher-level lifecycle state.
//
// The deterministic object identity is read first. A missing object may be
// created. AlreadyExists is treated as a GET->CREATE race and must be resolved
// by a fresh GET plus the strict create-or-adopt classifier. A transport error
// after Create is returned to the caller; a later reconciliation can safely
// GET and adopt the side effect if the remote create actually committed.
func ensureProjectedObject(
	ctx context.Context,
	client projectedObjectClient,
	expected *unstructured.Unstructured,
) (existingObjectAction, error) {
	if client == nil {
		return "", fmt.Errorf("Volcano projected-object client is required")
	}
	// Validate the complete expected identity even when the remote object is
	// currently absent. Creation must not be an escape hatch around the same
	// provider/generation/spec requirements used for adoption.
	if _, err := classifyExistingObject(expected, nil); err != nil {
		return "", err
	}

	current, err := client.Get(ctx, expected)
	if err == nil {
		action, classifyErr := classifyExistingObject(expected, current)
		if classifyErr != nil {
			return "", classifyErr
		}
		return action, nil
	}
	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("get deterministic Volcano object: %w", err)
	}

	created, err := client.Create(ctx, expected.DeepCopy())
	if err == nil {
		if created != nil {
			if _, classifyErr := classifyExistingObject(expected, created); classifyErr != nil {
				return "", fmt.Errorf("created Volcano object violates expected projection: %w", classifyErr)
			}
		}
		return existingObjectCreate, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		// This intentionally preserves the transport/provider error. If the
		// remote create committed but its acknowledgement was lost, the next
		// reconciliation will take the GET/adopt path instead of blind retry.
		return "", fmt.Errorf("create deterministic Volcano object: %w", err)
	}

	current, err = client.Get(ctx, expected)
	if err != nil {
		return "", fmt.Errorf("re-get Volcano object after AlreadyExists: %w", err)
	}
	action, err := classifyExistingObject(expected, current)
	if err != nil {
		return "", err
	}
	return action, nil
}
