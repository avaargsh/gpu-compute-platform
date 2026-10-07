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

func TestDynamicProjectedObjectClientDeleteThenGone(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil { t.Fatal(err) }
	client := fakeVolcanoDynamicClient()
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil { t.Fatal(err) }
	if _, err := ensureProjectedObject(context.Background(), transport, expected); err != nil { t.Fatal(err) }

	gone, err := deleteProjectedObject(context.Background(), transport, expected)
	if err != nil { t.Fatal(err) }
	if !gone { t.Fatal("expected VolcanoJob to be observed gone after delete") }

	gone, err = deleteProjectedObject(context.Background(), transport, expected)
	if err != nil || !gone {
		t.Fatalf("replayed delete gone=%v err=%v", gone, err)
	}
}
