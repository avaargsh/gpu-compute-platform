package volcano

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type queueCASTestClient struct {
	object        *unstructured.Unstructured
	lastUpdate    *unstructured.Unstructured
	getCalls      int
	updateCalls   int
	failConflict  bool
	lostAck       bool
	postACKObject *unstructured.Unstructured
}

func (c *queueCASTestClient) Get(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.getCalls++
	if c.object == nil {
		return nil, apierrors.NewNotFound(
			schema.GroupResource{Group: volcanoQueueGVR.Group, Resource: volcanoQueueGVR.Resource},
			expected.GetName(),
		)
	}
	return c.object.DeepCopy(), nil
}

func (c *queueCASTestClient) Create(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.object = expected.DeepCopy()
	c.object.SetUID("queue-test-uid")
	c.object.SetResourceVersion("10")
	return c.object.DeepCopy(), nil
}

func (c *queueCASTestClient) Delete(context.Context, *unstructured.Unstructured) error {
	return errors.New("DELETE should not run during Queue CAS test")
}

func (c *queueCASTestClient) Update(
	_ context.Context,
	candidate *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.updateCalls++
	c.lastUpdate = candidate.DeepCopy()
	if c.failConflict {
		c.failConflict = false
		return nil, apierrors.NewConflict(
			schema.GroupResource{Group: volcanoQueueGVR.Group, Resource: volcanoQueueGVR.Resource},
			candidate.GetName(), errors.New("stale resourceVersion"),
		)
	}
	if candidate.GetResourceVersion() != c.object.GetResourceVersion() ||
		candidate.GetUID() != c.object.GetUID() {
		return nil, apierrors.NewConflict(
			schema.GroupResource{Group: volcanoQueueGVR.Group, Resource: volcanoQueueGVR.Resource},
			candidate.GetName(), errors.New("concurrent replacement"),
		)
	}
	status := c.object.Object["status"]
	c.object = candidate.DeepCopy()
	c.object.SetResourceVersion("11")
	if status != nil {
		c.object.Object["status"] = status
	}
	if c.lostAck {
		c.lostAck = false
		return nil, context.DeadlineExceeded
	}
	// Simulate a successful API response followed by an independent GET that
	// observes a stale or foreign object. The provider must reject this drift
	// instead of promoting the UPDATE response to an observed generation.
	ack := c.object.DeepCopy()
	if c.postACKObject != nil {
		c.object = c.postACKObject.DeepCopy()
	}
	return ack, nil
}

func oldQueueWithServerIdentity(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	q, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	q.SetUID("queue-test-uid")
	q.SetResourceVersion("10")
	q.Object["status"] = map[string]any{"state": "Open"}
	return q
}

func nextQueueProjection() baseprovider.PoolProjection {
	next := adoptionPoolProjection()
	next.Generation = 5
	next.Accelerators = []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 16}}
	return next
}

func TestVolcanoQueueCASAdvancePreservesControllerMetadata(t *testing.T) {
	old := oldQueueWithServerIdentity(t)
	annotations := old.GetAnnotations()
	annotations["volcano.sh/controller-note"] = "preserve-me"
	old.SetAnnotations(annotations)
	old.SetLabels(map[string]string{"team": "data"})
	old.SetFinalizers([]string{"volcano.sh/protect"})
	// Queue is cluster-scoped; use a cluster-scoped owner kind in the fixture.
	old.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: "external", UID: "parent-uid"}})
	old.Object["spec"].(map[string]any)["parent"] = "root"
	client := &queueCASTestClient{object: old}
	p := newProvider(client)

	observed, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err != nil {
		t.Fatal(err)
	}
	if observed.ObservedGeneration != 5 || client.updateCalls != 1 ||
		len(observed.Conditions) != 2 ||
		observed.Conditions[0].Status != "True" ||
		observed.Conditions[1].Type != "QuotaApplied" ||
		observed.Conditions[1].Status != "Unknown" {
		t.Fatalf("Queue CAS did not converge: %#v updateCalls=%d", observed, client.updateCalls)
	}
	if client.lastUpdate.Object["spec"].(map[string]any)["parent"] != "root" {
		t.Fatal("reviewed Volcano Queue default should survive quota update")
	}
	if client.lastUpdate.GetResourceVersion() != "10" ||
		client.lastUpdate.GetUID() != "queue-test-uid" {
		t.Fatalf("Queue CAS lost identity fence: %#v", client.lastUpdate)
	}
	if _, ok := client.lastUpdate.Object["status"]; ok {
		t.Fatal("Queue spec writer must not submit Volcano controller status")
	}
	current := client.object
	if current.GetAnnotations()[generationAnnotation] != "5" ||
		current.GetAnnotations()["volcano.sh/controller-note"] != "preserve-me" ||
		current.GetLabels()["team"] != "data" ||
		len(current.GetFinalizers()) != 1 || len(current.GetOwnerReferences()) != 1 {
		t.Fatalf("Queue generation or controller metadata lost: %#v", current)
	}
	capability, _, err := unstructured.NestedStringMap(current.Object, "spec", "capability")
	if err != nil || capability["nvidia.com/gpu"] != "16" {
		t.Fatalf("Queue capacity did not advance: %#v %v", capability, err)
	}
	replayed, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err != nil || replayed.ObservedGeneration != 5 || client.updateCalls != 1 {
		t.Fatalf("same-generation replay performed duplicate UPDATE: %#v err=%v updates=%d", replayed, err, client.updateCalls)
	}
	if len(replayed.Conditions) != 2 ||
		replayed.Conditions[1].Type != "QuotaApplied" ||
		replayed.Conditions[1].Status != "Unknown" {
		t.Fatalf("replayed readback must not invent scheduler-applied quota: %#v", replayed.Conditions)
	}
}

func TestVolcanoQueueCASLostAckAdoptsCommittedGeneration(t *testing.T) {
	client := &queueCASTestClient{object: oldQueueWithServerIdentity(t), lostAck: true}
	p := newProvider(client)
	_, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || !baseprovider.IsRetryable(err) || client.updateCalls != 1 {
		t.Fatalf("lost UPDATE ACK should be retryable: err=%v calls=%d", err, client.updateCalls)
	}
	observed, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err != nil || observed.ObservedGeneration != 5 || client.updateCalls != 1 {
		t.Fatalf("lost ACK replay must adopt, not update again: %#v err=%v calls=%d", observed, err, client.updateCalls)
	}
}

func TestVolcanoQueueCASSuccessACKStaleReadbackFailsClosed(t *testing.T) {
	old := oldQueueWithServerIdentity(t)
	client := &queueCASTestClient{
		object:        old.DeepCopy(),
		postACKObject: old.DeepCopy(),
	}
	p := newProvider(client)
	observation, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || observation.ObservedGeneration != 0 ||
		client.updateCalls != 1 || client.getCalls < 2 {
		t.Fatalf("stale independent GET must not be reported as CAS success: observation=%#v err=%v updates=%d gets=%d",
			observation, err, client.updateCalls, client.getCalls)
	}
}

func TestVolcanoQueueCASSuccessACKForeignReadbackFailsClosed(t *testing.T) {
	old := oldQueueWithServerIdentity(t)
	foreign := old.DeepCopy()
	annotations := foreign.GetAnnotations()
	annotations[poolIDAnnotation] = "foreign-pool"
	foreign.SetAnnotations(annotations)
	foreign.SetUID("foreign-queue-uid")
	foreign.SetResourceVersion("12")
	client := &queueCASTestClient{
		object:        old,
		postACKObject: foreign,
	}
	p := newProvider(client)
	observation, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || baseprovider.IsRetryable(err) ||
		observation.ObservedGeneration != 0 || client.updateCalls != 1 {
		t.Fatalf("foreign identity after successful ACK must fail closed: observation=%#v err=%v updates=%d",
			observation, err, client.updateCalls)
	}
}

func TestVolcanoQueueCASSuccessACKRecreatedSameProjectionFailsClosed(t *testing.T) {
	old := oldQueueWithServerIdentity(t)
	// The replacement is observationally indistinguishable by name, generation,
	// quota and platform owner annotations. Only Kubernetes UID identifies the
	// object that actually acknowledged our UPDATE.
	replacement, err := ProjectPool(nextQueueProjection())
	if err != nil {
		t.Fatal(err)
	}
	replacement.SetUID("recreated-queue-uid")
	replacement.SetResourceVersion("12")
	replacement.Object["status"] = map[string]any{"state": "Open"}
	client := &queueCASTestClient{
		object:        old,
		postACKObject: replacement,
	}
	p := newProvider(client)
	observation, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || baseprovider.IsRetryable(err) ||
		observation.ObservedGeneration != 0 ||
		client.updateCalls != 1 || client.getCalls < 2 {
		t.Fatalf("same-projection replacement UID must reject ownership: observation=%#v err=%v updates=%d gets=%d",
			observation, err, client.updateCalls, client.getCalls)
	}
}

func TestVolcanoQueueCASConflictRetriesFromFreshGET(t *testing.T) {
	client := &queueCASTestClient{object: oldQueueWithServerIdentity(t), failConflict: true}
	p := newProvider(client)
	_, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || !apierrors.IsConflict(err) || !baseprovider.IsRetryable(err) {
		t.Fatalf("CAS conflict must return retryable: %v", err)
	}
	reads := client.getCalls
	_, err = p.ReconcilePool(context.Background(), nextQueueProjection())
	if err != nil || client.getCalls <= reads || client.updateCalls != 2 {
		t.Fatalf("retry must re-read before a new CAS: err=%v GETs=%d updates=%d", err, client.getCalls, client.updateCalls)
	}
}

func TestVolcanoQueueCASRejectsForeignTakeoverAfterConflict(t *testing.T) {
	client := &queueCASTestClient{object: oldQueueWithServerIdentity(t), failConflict: true}
	p := newProvider(client)
	_, err := p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || !baseprovider.IsRetryable(err) {
		t.Fatalf("expected retryable conflict: %v", err)
	}
	foreign := client.object.DeepCopy()
	annotations := foreign.GetAnnotations()
	annotations[poolIDAnnotation] = "other-pool"
	foreign.SetAnnotations(annotations)
	foreign.SetUID("foreign-uid")
	foreign.SetResourceVersion("11")
	client.object = foreign

	_, err = p.ReconcilePool(context.Background(), nextQueueProjection())
	if err == nil || baseprovider.IsRetryable(err) || client.updateCalls != 1 {
		t.Fatalf("foreign takeover must fail closed without retrying UPDATE: err=%v updates=%d", err, client.updateCalls)
	}
}

func TestVolcanoQueueCASSameGenerationSpecDriftFailsClosed(t *testing.T) {
	client := &queueCASTestClient{object: oldQueueWithServerIdentity(t)}
	desired := adoptionPoolProjection()
	desired.Accelerators = []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 16}}
	_, err := newProvider(client).ReconcilePool(context.Background(), desired)
	if err == nil || baseprovider.IsRetryable(err) || client.updateCalls != 0 {
		t.Fatalf("same-generation spec change must fail closed: err=%v updates=%d", err, client.updateCalls)
	}
}

func TestVolcanoQueueCASRejectsUnreviewedOrMalformedExistingQueue(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{"newer generation", func(o *unstructured.Unstructured) {
			a := o.GetAnnotations()
			a[generationAnnotation] = "6"
			o.SetAnnotations(a)
		}},
		{"invalid existing generation", func(o *unstructured.Unstructured) {
			a := o.GetAnnotations()
			a[generationAnnotation] = "invalid"
			o.SetAnnotations(a)
		}},
		{"extra platform annotation", func(o *unstructured.Unstructured) {
			a := o.GetAnnotations()
			a["ai.compute/new-owner"] = "foreign"
			o.SetAnnotations(a)
		}},
		{"foreign platform label", func(o *unstructured.Unstructured) {
			o.SetLabels(map[string]string{"ai.compute/scheduler": "foreign"})
		}},
		{"empty foreign platform label", func(o *unstructured.Unstructured) {
			o.SetLabels(map[string]string{"ai.compute/scheduler": ""})
		}},
		{"queue spec injection", func(o *unstructured.Unstructured) {
			o.Object["spec"].(map[string]any)["affinity"] = map[string]any{"unsafe": true}
		}},
		{"bad default value", func(o *unstructured.Unstructured) {
			o.Object["spec"].(map[string]any)["parent"] = "different-parent"
		}},
		{"extra resource key", func(o *unstructured.Unstructured) {
			o.Object["spec"].(map[string]any)["capability"].(map[string]any)["example.com/gpu"] = "9"
		}},
		{"negative old quota", func(o *unstructured.Unstructured) {
			o.Object["spec"].(map[string]any)["capability"].(map[string]any)["nvidia.com/gpu"] = "-1"
		}},
		{"missing UID", func(o *unstructured.Unstructured) { o.SetUID("") }},
		{"missing RV", func(o *unstructured.Unstructured) { o.SetResourceVersion("") }},
		{"terminating Queue", func(o *unstructured.Unstructured) {
			now := metav1.Now()
			o.SetDeletionTimestamp(&now)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := oldQueueWithServerIdentity(t)
			tt.mutate(queue)
			client := &queueCASTestClient{object: queue}
			_, err := newProvider(client).ReconcilePool(context.Background(), nextQueueProjection())
			if err == nil || baseprovider.IsRetryable(err) || client.updateCalls != 0 {
				t.Fatalf("unreviewed Queue must not be promoted: err=%v updates=%d", err, client.updateCalls)
			}
		})
	}
}

func TestVolcanoQueueCASDoesNotChangeImmutableJobReconciliation(t *testing.T) {
	// This project keeps Job reconciliation on ensureProjectedObject. There is
	// intentionally no UPDATE route in its dynamic client transport.
	client := fakeVolcanoDynamicClient()
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Update(context.Background(), expected); err == nil {
		t.Fatal("Volcano Job UPDATE must not be permitted")
	}
}
