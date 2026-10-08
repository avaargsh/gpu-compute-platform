package volcano

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func fakeVolcanoDynamicClient() *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			volcanoQueueGVR: "QueueList",
			volcanoJobGVR:   "JobList",
		},
	)
}

func TestDynamicProjectedObjectClientDrivesEnsureForVolcanoJob(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := fakeVolcanoDynamicClient()
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}

	action, err := ensureProjectedObject(context.Background(), transport, expected)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectCreate {
		t.Fatalf("first ensure action=%q, want create", action)
	}

	got, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetAnnotations()[providerAnnotation] != ProviderName ||
		got.GetAnnotations()[generationAnnotation] != expected.GetAnnotations()[generationAnnotation] {
		t.Fatalf("stored VolcanoJob lost provider identity: %#v", got.GetAnnotations())
	}

	action, err = ensureProjectedObject(context.Background(), transport, expected)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectAdopt {
		t.Fatalf("replay action=%q, want adopt", action)
	}
}

func TestDynamicProjectedObjectClientDrivesEnsureForVolcanoQueue(t *testing.T) {
	expected, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := fakeVolcanoDynamicClient()
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ensureProjectedObject(context.Background(), transport, expected); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resource(volcanoQueueGVR).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicProjectedObjectClientReturnsNativeNotFound(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := fakeVolcanoDynamicClient()
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}

	_, err = transport.Get(context.Background(), expected)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Get error=%v, want Kubernetes NotFound", err)
	}
}

func TestDynamicProjectedObjectClientRejectsUnsupportedOrMisScopedObjects(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}

	unsupported := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "not-volcano"},
		"spec":       map[string]any{},
	}}
	if _, err := transport.Get(context.Background(), unsupported); err == nil {
		t.Fatal("unsupported GVK must fail closed")
	}

	job, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	job.SetNamespace("")
	if _, err := transport.Get(context.Background(), job); err == nil {
		t.Fatal("namespaced VolcanoJob without namespace must fail closed")
	}

	queue, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	queue.SetNamespace("should-not-exist")
	if _, err := transport.Get(context.Background(), queue); err == nil {
		t.Fatal("cluster-scoped Volcano Queue with namespace must fail closed")
	}
}

func TestDynamicProjectedObjectClientDeleteConfirmsGoneForVolcanoJob(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := fakeVolcanoDynamicClient()
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ensureProjectedObject(
		context.Background(),
		transport,
		expected,
	); err != nil {
		t.Fatal(err)
	}

	// Unlike a real API server, the dynamic fake does not allocate metadata
	// on CREATE. Seed it explicitly; production DELETE must never fall back
	// to an unguarded name-only operation.
	resource := client.Resource(volcanoJobGVR).Namespace(expected.GetNamespace())
	stored, err := resource.Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stored.SetUID("volcano-job-uid")
	stored.SetResourceVersion("17")
	if _, err := resource.Update(context.Background(), stored, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	gone, err := deleteProjectedObject(
		context.Background(),
		transport,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !gone {
		t.Fatal("dynamic fake delete should be observed as gone")
	}

	_, err = client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("deleted VolcanoJob error=%v, want NotFound", err)
	}

	var deleteCalls int
	for _, action := range client.Actions() {
		if !action.Matches("delete", "jobs") {
			continue
		}
		deleteCalls++
		deletion, ok := action.(k8stesting.DeleteAction)
		if !ok {
			t.Fatalf("unexpected DELETE action type: %T", action)
		}
		preconditions := deletion.GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil ||
			*preconditions.UID != "volcano-job-uid" ||
			preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != "17" {
			t.Fatalf("Volcano DELETE missing UID/RV preconditions: %#v", preconditions)
		}
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls=%d, want 1", deleteCalls)
	}
}

func TestDynamicProjectedObjectClientObserveReadsVolcanoJobPhase(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	existing := expected.DeepCopy()
	existing.SetUID("job-uid")
	existing.Object["status"] = map[string]any{
		"state": map[string]any{
			"phase": "Running",
		},
	}

	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			volcanoQueueGVR: "QueueList",
			volcanoJobGVR:   "JobList",
		},
		existing,
	)
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}

	observation, err := observeProjectedObject(
		context.Background(),
		transport,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Exists || observation.Phase != "Running" || observation.UID != "job-uid" {
		t.Fatalf("unexpected dynamic observation: %#v", observation)
	}
}
