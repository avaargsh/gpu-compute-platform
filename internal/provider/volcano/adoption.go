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
// adoptable only when the provider-owned identity markers match and every
// immutable field emitted by our projection is still present with the same
// value. Only explicitly reviewed API server / Volcano defaults may be
// additional. Runtime metadata and status are ignored; platform labels are not.
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
	if !projectionSubsetMatches(expected.GetKind(), expectedSpec, existingSpec) {
		return "", providerObjectConflict("immutable provider projection differs")
	}

	return existingObjectAdopt, nil
}

// projectionSubsetMatches preserves every projected field and allows only
// explicitly reviewed server defaults. A generic map-subset check is unsafe:
// injected hostNetwork, nodeSelector or GPU requests could otherwise be
// adopted as our own immutable workload at the same desired generation.
func projectionSubsetMatches(kind string, expected, existing any) bool {
	return matchProjectedSpec(kind, "spec", expected, existing)
}

func matchProjectedSpec(kind, path string, expected, existing any) bool {
	switch want := expected.(type) {
	case map[string]any:
		got, ok := existing.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range want {
			actual, exists := got[key]
			if !exists || !matchProjectedSpec(kind, path+"."+key, value, actual) {
				return false
			}
		}
		for key, actual := range got {
			if _, projected := want[key]; !projected &&
				!allowedVolcanoDefault(kind, path, key, actual) {
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
			if !matchProjectedSpec(kind, path+"[]", want[i], got[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(want, existing)
	}
}

// Default values are intentionally pinned to the narrow Stage B experiment.
// A Volcano/Kubernetes upgrade that adds or changes a default must fail
// closed until a real API-server observation and a review update this list.
func allowedVolcanoDefault(kind, path, key string, value any) bool {
	switch {
	case kind == "Queue" && path == "spec":
		switch key {
		case "parent":
			return value == "root"
		case "reclaimable":
			return value == false
		case "weight":
			return reflect.DeepEqual(value, int64(1))
		}
	case kind == "Job" && path == "spec" && key == "maxRetry":
		return reflect.DeepEqual(value, int64(3))
	case kind == "Job" && path == "spec.tasks[].template.spec":
		switch key {
		case "dnsPolicy":
			return value == "ClusterFirst"
		case "terminationGracePeriodSeconds":
			return reflect.DeepEqual(value, int64(30))
		}
	}
	return false
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
