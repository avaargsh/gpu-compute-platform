package volcano

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const existingObjectUpdate existingObjectAction = "update"

// queueObjectUpdater intentionally remains outside the core provider transport:
// older fake clients and immutable Job paths must never acquire an UPDATE path.
type queueObjectUpdater interface {
	Update(context.Context, *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

// ensurePoolQueue admits only a monotonic generation change for the same Queue
// identity. UPDATE is a single resourceVersion-CAS attempt. Any uncertainty is
// returned to the Agent, which restarts reconciliation from GET.
func (p *Provider) ensurePoolQueue(
	ctx context.Context,
	expected *unstructured.Unstructured,
) (existingObjectAction, error) {
	if _, err := classifyExistingObject(expected, nil); err != nil {
		return "", err
	}
	current, err := p.client.Get(ctx, expected)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ensureProjectedObject(ctx, p.client, expected)
		}
		return "", err
	}
	want, have, err := validateQueueCASIdentity(expected, current)
	if err != nil {
		return "", err
	}
	if have == want {
		return classifyExistingObject(expected, current)
	}
	if have > want {
		return "", providerObjectConflict("Queue generation rollback: have %d want %d", have, want)
	}
	updater, ok := p.client.(queueObjectUpdater)
	if !ok {
		return "", fmt.Errorf("Volcano Queue CAS transport is unavailable")
	}
	if current.GetUID() == "" || current.GetResourceVersion() == "" {
		return "", providerObjectConflict("Queue UPDATE requires observed UID and resourceVersion")
	}

	candidate := current.DeepCopy()
	// Preserve only already-reviewed controller defaults in the existing spec;
	// change the one projected mutable field, accelerator capability.
	capability, _, err := unstructured.NestedStringMap(expected.Object, "spec", "capability")
	if err != nil {
		return "", err
	}
	if err := unstructured.SetNestedStringMap(candidate.Object, capability, "spec", "capability"); err != nil {
		return "", err
	}
	annotations := candidate.GetAnnotations()
	annotations[generationAnnotation] = expected.GetAnnotations()[generationAnnotation]
	candidate.SetAnnotations(annotations)
	// Status belongs to the Volcano controller, not the Queue spec writer.
	delete(candidate.Object, "status")

	updated, err := updater.Update(ctx, candidate)
	if err != nil {
		return "", err
	}
	if updated == nil || updated.GetUID() != current.GetUID() {
		return "", providerObjectConflict("Queue UPDATE returned missing or changed UID")
	}
	if _, err := classifyExistingObject(expected, updated); err != nil {
		return "", fmt.Errorf("Queue UPDATE response is not the desired generation: %w", err)
	}
	return existingObjectUpdate, nil
}

// A prior generation is not free-form mutable state. Only its projected
// accelerator quota may differ. Unknown platform metadata, extra spec knobs,
// wrong binding resource, or modified Volcano defaults block promotion.
func validateQueueCASIdentity(expected, current *unstructured.Unstructured) (int64, int64, error) {
	if current == nil || expected.GetKind() != "Queue" ||
		current.GetKind() != "Queue" ||
		expected.GetAPIVersion() != current.GetAPIVersion() ||
		expected.GetName() != current.GetName() ||
		expected.GetNamespace() != current.GetNamespace() ||
		current.GetDeletionTimestamp() != nil {
		return 0, 0, providerObjectConflict("Queue GVK/name identity or deletion state differs")
	}
	wantAnnotations, haveAnnotations := expected.GetAnnotations(), current.GetAnnotations()
	for _, key := range []string{providerAnnotation, poolIDAnnotation, acceleratorClassAnnotation} {
		if wantAnnotations[key] == "" || haveAnnotations[key] != wantAnnotations[key] {
			return 0, 0, providerObjectConflict("Queue identity annotation %s differs", key)
		}
	}
	for key := range haveAnnotations {
		if strings.HasPrefix(key, "ai.compute/") {
			if _, ok := wantAnnotations[key]; !ok {
				return 0, 0, providerObjectConflict("unexpected provider-owned Queue annotation %s", key)
			}
		}
	}
	for key, value := range current.GetLabels() {
		if !strings.HasPrefix(key, "ai.compute/") {
			continue
		}
		want, known := expected.GetLabels()[key]
		if !known || want != value {
			return 0, 0, providerObjectConflict("unexpected provider-owned Queue label %s", key)
		}
	}
	want, err := strconv.ParseInt(wantAnnotations[generationAnnotation], 10, 64)
	if err != nil || want <= 0 {
		return 0, 0, fmt.Errorf("invalid desired Queue generation")
	}
	have, err := strconv.ParseInt(haveAnnotations[generationAnnotation], 10, 64)
	if err != nil || have <= 0 {
		return 0, 0, providerObjectConflict("invalid stored Queue generation")
	}

	expectedCapability, found, err := unstructured.NestedStringMap(expected.Object, "spec", "capability")
	if err != nil || !found || len(expectedCapability) != 1 {
		return 0, 0, fmt.Errorf("invalid desired Queue accelerator capability")
	}
	currentCapability, found, err := unstructured.NestedStringMap(current.Object, "spec", "capability")
	if err != nil || !found || len(currentCapability) != 1 {
		return 0, 0, providerObjectConflict("invalid stored Queue accelerator capability")
	}
	var resource string
	for key := range expectedCapability {
		resource = key
	}
	oldQuota, ok := currentCapability[resource]
	if !ok {
		return 0, 0, providerObjectConflict("Queue accelerator binding changed")
	}
	oldCount, err := strconv.ParseInt(oldQuota, 10, 64)
	if err != nil || oldCount <= 0 {
		return 0, 0, providerObjectConflict("Queue accelerator quota is invalid")
	}
	oldProjection := expected.DeepCopy()
	if err := unstructured.SetNestedStringMap(oldProjection.Object, currentCapability, "spec", "capability"); err != nil {
		return 0, 0, err
	}
	if !projectionSubsetMatches("Queue", oldProjection.Object["spec"], current.Object["spec"]) {
		return 0, 0, providerObjectConflict("Queue has unreviewed spec drift beyond quota")
	}
	return want, have, nil
}
