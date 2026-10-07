package volcano

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type existingObjectAction string

const (
	existingObjectCreate existingObjectAction = "create"
	existingObjectAdopt  existingObjectAction = "adopt"
)

// classifyExistingObject is the Stage B create-or-adopt decision boundary.
//
// A missing deterministic provider object may be created. An existing object is
// adoptable only when the provider-owned identity markers and immutable
// projected spec match exactly. Kubernetes/controller metadata and status are
// intentionally ignored; provider-owned ai.compute/* labels are not.
//
// This helper does not perform remote side effects. A future Volcano client must
// call it after GET and again after an AlreadyExists race before reporting an
// existing object as the current desired generation.
func classifyExistingObject(expected, existing *unstructured.Unstructured) (existingObjectAction, error) {
	if expected == nil {
		return "", fmt.Errorf("expected provider object is required")
	}
	if existing == nil {
		return existingObjectCreate, nil
	}

	if expected.GetKind() != "Queue" && expected.GetKind() != "Job" {
		return "", fmt.Errorf("unsupported Volcano provider object kind %q", expected.GetKind())
	}
	if expected.GetAPIVersion() != existing.GetAPIVersion() ||
		expected.GetKind() != existing.GetKind() {
		return "", providerObjectConflict(
			"GVK mismatch: expected %s %s, got %s %s",
			expected.GetAPIVersion(), expected.GetKind(),
			existing.GetAPIVersion(), existing.GetKind(),
		)
	}
	if expected.GetName() != existing.GetName() ||
		expected.GetNamespace() != existing.GetNamespace() {
		return "", providerObjectConflict(
			"identity mismatch: expected %s/%s, got %s/%s",
			expected.GetNamespace(), expected.GetName(),
			existing.GetNamespace(), existing.GetName(),
		)
	}

	expectedAnnotations := expected.GetAnnotations()
	existingAnnotations := existing.GetAnnotations()
	for _, key := range providerOwnedAnnotationKeys(expected.GetKind()) {
		want := expectedAnnotations[key]
		if want == "" {
			return "", fmt.Errorf("expected provider object is missing required annotation %q", key)
		}
		if got := existingAnnotations[key]; got != want {
			return "", providerObjectConflict(
				"annotation %s mismatch: expected %q, got %q",
				key, want, got,
			)
		}
	}

	expectedLabels := expected.GetLabels()
	existingLabels := existing.GetLabels()
	for key, want := range expectedLabels {
		if !strings.HasPrefix(key, "ai.compute/") {
			continue
		}
		if got := existingLabels[key]; got != want {
			return "", providerObjectConflict(
				"label %s mismatch: expected %q, got %q",
				key, want, got,
			)
		}
	}

	expectedSpec, expectedFound, err := unstructured.NestedFieldNoCopy(expected.Object, "spec")
	if err != nil || !expectedFound {
		return "", fmt.Errorf("expected provider object spec is required: found=%t err=%v", expectedFound, err)
	}
	existingSpec, existingFound, err := unstructured.NestedFieldNoCopy(existing.Object, "spec")
	if err != nil || !existingFound {
		return "", providerObjectConflict("existing provider object spec is missing")
	}
	if !reflect.DeepEqual(expectedSpec, existingSpec) {
		return "", providerObjectConflict("immutable provider projection differs")
	}

	return existingObjectAdopt, nil
}

func providerOwnedAnnotationKeys(kind string) []string {
	if kind == "Queue" {
		return []string{
			providerAnnotation,
			generationAnnotation,
			poolIDAnnotation,
			acceleratorClassAnnotation,
		}
	}
	return []string{
		providerAnnotation,
		generationAnnotation,
		poolIDAnnotation,
		workloadIDAnnotation,
		acceleratorClassAnnotation,
	}
}

func providerObjectConflict(format string, args ...any) error {
	return fmt.Errorf("provider object conflict: "+format, args...)
}
