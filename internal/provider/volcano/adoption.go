package volcano

import (
	"fmt"
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
	if expected.GetKind() != "Queue" && expected.GetKind() != "Job" {
		return "", fmt.Errorf("unsupported Volcano provider object kind %q", expected.GetKind())
	}
	if expected.GetAPIVersion() == "" || expected.GetName() == "" {
		return "", fmt.Errorf("expected provider object GVK/name identity is required")
	}
	if expected.GetKind() == "Job" && expected.GetNamespace() == "" {
		return "", fmt.Errorf("expected Volcano Job namespace is required")
	}
	requiredAnnotations := providerOwnedAnnotationKeys(expected.GetKind())
	expectedAnnotations := expected.GetAnnotations()
	for _, key := range requiredAnnotations {
		if expectedAnnotations[key] == "" {
			return "", fmt.Errorf("expected provider object is missing required annotation %q", key)
		}
	}
	expectedSpec, expectedFound, err := unstructured.NestedFieldNoCopy(expected.Object, "spec")
	if err != nil || !expectedFound {
		return "", fmt.Errorf("expected provider object spec is required: found=%t err=%v", expectedFound, err)
	}
	if existing == nil {
		return existingObjectCreate, nil
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

	existingAnnotations := existing.GetAnnotations()
	for _, key := range requiredAnnotations {
		want := expectedAnnotations[key]
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

	existingSpec, existingFound, err := unstructured.NestedFieldNoCopy(existing.Object, "spec")
	if err != nil || !existingFound {
		return "", providerObjectConflict("existing provider object spec is missing")
	}
	if !projectionSubsetMatches(expectedSpec, existingSpec) {
		return "", providerObjectConflict("immutable provider projection differs")
	}

	return existingObjectAdopt, nil
}

// projectionSubsetMatches requires the live object to preserve every field
// emitted by our deterministic provider projection while allowing the API
// server or Volcano admission/defaulting to add extra map fields. Slice shape
// stays exact so extra tasks/containers cannot be silently adopted.
func projectionSubsetMatches(expected, existing any) bool {
	switch want := expected.(type) {
	case map[string]any:
		got, ok := existing.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range want {
			actual, exists := got[key]
			if !exists || !projectionSubsetMatches(value, actual) {
				return false
			}
		}
		return true
	case []any:
		got, ok := existing.([]any)
		if !ok || len(want) != len(got) {
			return false
		}
		for i := range want {
			if !projectionSubsetMatches(want[i], got[i]) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprint(want) == fmt.Sprint(existing)
	}
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
