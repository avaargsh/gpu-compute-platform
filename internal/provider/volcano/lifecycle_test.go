package volcano

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type fakeLifecycleClient struct {
	object                 *unstructured.Unstructured
	getErr                 error
	deleteErr              error
	deleteSideEffect       bool
	deleteCalls            int
	omitServerIdentity     bool
	replacementOnDelete    *unstructured.Unstructured
	deletedUID             types.UID
	deletedResourceVersion string
}

func (f *fakeLifecycleClient) Get(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.object == nil {
		return nil, apierrors.NewNotFound(
			schema.GroupResource{
				Group:    expected.GroupVersionKind().Group,
				Resource: expected.GetKind(),
			},
			expected.GetName(),
		)
	}
	current := f.object.DeepCopy()
	// Kubernetes assigns these fields on persisted objects; the in-memory
	// projector/fake Create doesn't, so the test client supplies them on GET.
	if !f.omitServerIdentity {
		if current.GetUID() == "" {
			current.SetUID("test-volcano-uid")
		}
		if current.GetResourceVersion() == "" {
			current.SetResourceVersion("1")
		}
	}
	return current, nil
}

func (f *fakeLifecycleClient) Create(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	f.object = expected.DeepCopy()
	return f.object.DeepCopy(), nil
}

func (f *fakeLifecycleClient) Delete(
	_ context.Context,
	observed *unstructured.Unstructured,
) error {
	f.deleteCalls++
	f.deletedUID = observed.GetUID()
	f.deletedResourceVersion = observed.GetResourceVersion()
	if f.replacementOnDelete != nil {
		f.object = f.replacementOnDelete.DeepCopy()
	} else if f.deleteSideEffect {
		f.object = nil
	}
	return f.deleteErr
}

func TestObserveProjectedObjectReturnsIndependentVolcanoJobReality(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	existing := expected.DeepCopy()
	existing.SetUID(types.UID("volcano-job-uid"))
	existing.Object["status"] = map[string]any{
		"state": map[string]any{
			"phase": "Running",
		},
	}
	client := &fakeLifecycleClient{object: existing}

	observation, err := observeProjectedObject(
		context.Background(),
		client,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Exists ||
		observation.UID != types.UID("volcano-job-uid") ||
		observation.Phase != "Running" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func TestObserveProjectedObjectRejectsConflictingGeneration(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	conflict := expected.DeepCopy()
	annotations := conflict.GetAnnotations()
	annotations[generationAnnotation] = "999"
	conflict.SetAnnotations(annotations)

	_, err = observeProjectedObject(
		context.Background(),
		&fakeLifecycleClient{object: conflict},
		expected,
	)
	if err == nil {
		t.Fatal("observation must not accept a conflicting generation")
	}
}

func TestObserveProjectedObjectReportsMissingWithoutCreating(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{}

	observation, err := observeProjectedObject(
		context.Background(),
		client,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Exists {
		t.Fatalf("missing provider object reported as existing: %#v", observation)
	}
	if client.deleteCalls != 0 {
		t.Fatal("observation must not mutate provider state")
	}
}

func TestDeleteProjectedObjectConfirmsGoneOnlyAfterObservation(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}

	pending := &fakeLifecycleClient{
		object: expected.DeepCopy(),
	}
	gone, err := deleteProjectedObject(
		context.Background(),
		pending,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if gone {
		t.Fatal("DELETE acknowledgement alone must not mean Gone")
	}
	if pending.deleteCalls != 1 {
		t.Fatalf("delete calls=%d, want 1", pending.deleteCalls)
	}

	converged := &fakeLifecycleClient{
		object:           expected.DeepCopy(),
		deleteSideEffect: true,
	}
	gone, err = deleteProjectedObject(
		context.Background(),
		converged,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !gone || converged.deleteCalls != 1 {
		t.Fatalf(
			"confirmed delete: gone=%t deleteCalls=%d",
			gone,
			converged.deleteCalls,
		)
	}
}

func TestDeleteProjectedObjectLostAckReplaysToGone(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{
		object:           expected.DeepCopy(),
		deleteSideEffect: true,
		deleteErr:        context.DeadlineExceeded,
	}

	gone, err := deleteProjectedObject(
		context.Background(),
		client,
		expected,
	)
	if gone || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost delete ACK: gone=%t err=%v", gone, err)
	}
	if client.object != nil || client.deleteCalls != 1 {
		t.Fatalf(
			"remote delete side effect not simulated: object=%v deletes=%d",
			client.object != nil,
			client.deleteCalls,
		)
	}

	client.deleteErr = nil
	client.deleteSideEffect = false
	gone, err = deleteProjectedObject(
		context.Background(),
		client,
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !gone {
		t.Fatal("replay must converge from observed NotFound")
	}
	if client.deleteCalls != 1 {
		t.Fatalf(
			"replay should not issue a second delete after NotFound: deletes=%d",
			client.deleteCalls,
		)
	}
}

func TestDeleteProjectedObjectRejectsConflictBeforeDelete(t *testing.T) {
	expected, err := ProjectPool(adoptionPoolProjection())
	if err != nil {
		t.Fatal(err)
	}
	conflict := expected.DeepCopy()
	annotations := conflict.GetAnnotations()
	annotations[poolIDAnnotation] = "other-pool"
	conflict.SetAnnotations(annotations)
	client := &fakeLifecycleClient{
		object:           conflict,
		deleteSideEffect: true,
	}

	if _, err := deleteProjectedObject(
		context.Background(),
		client,
		expected,
	); err == nil {
		t.Fatal("conflicting provider object must fail closed before DELETE")
	}
	if client.deleteCalls != 0 {
		t.Fatalf(
			"conflicting provider object was deleted: deletes=%d",
			client.deleteCalls,
		)
	}
}

func TestDeleteProjectedObjectAmbiguousReadDoesNotDeleteBlindly(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{
		getErr:           context.DeadlineExceeded,
		deleteSideEffect: true,
	}

	if _, err := deleteProjectedObject(
		context.Background(),
		client,
		expected,
	); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("get error=%v, want deadline exceeded", err)
	}
	if client.deleteCalls != 0 {
		t.Fatalf(
			"ambiguous GET triggered DELETE: deletes=%d",
			client.deleteCalls,
		)
	}
}

func TestDeleteProjectedObjectUsesVerifiedServerIdentity(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{object: expected.DeepCopy()}
	_, err = deleteProjectedObject(context.Background(), client, expected)
	if err != nil {
		t.Fatal(err)
	}
	if client.deletedUID != types.UID("test-volcano-uid") ||
		client.deletedResourceVersion != "1" {
		t.Fatalf("DELETE lost verified identity: UID=%q RV=%q", client.deletedUID, client.deletedResourceVersion)
	}
}

func TestDeleteProjectedObjectRefusesObjectsWithoutServerIdentity(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{
		object:             expected.DeepCopy(),
		omitServerIdentity: true,
	}
	gone, err := deleteProjectedObject(context.Background(), client, expected)
	if err == nil || gone || client.deleteCalls != 0 {
		t.Fatalf("missing UID/RV must fail before DELETE: gone=%t err=%v deletes=%d", gone, err, client.deleteCalls)
	}
}

func TestDeleteProjectedObjectNotFoundDoesNotHideReplacement(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	replacement := expected.DeepCopy()
	annotations := replacement.GetAnnotations()
	annotations[workloadIDAnnotation] = "foreign-workload"
	replacement.SetAnnotations(annotations)

	// The old object disappears between the authorized GET and DELETE,
	// while a foreign object takes the same deterministic name. NotFound
	// from DELETE cannot be treated as a convergence receipt.
	client := &fakeLifecycleClient{
		object:              expected.DeepCopy(),
		replacementOnDelete: replacement,
		deleteErr: apierrors.NewNotFound(
			schema.GroupResource{Group: volcanoJobGVR.Group, Resource: volcanoJobGVR.Resource},
			expected.GetName(),
		),
	}
	gone, err := deleteProjectedObject(context.Background(), client, expected)
	if gone || err == nil {
		t.Fatalf("name replacement must not report Gone: gone=%t err=%v", gone, err)
	}
	if client.deleteCalls != 1 {
		t.Fatalf("delete calls=%d, want 1", client.deleteCalls)
	}
}
