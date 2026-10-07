package volcano

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type fakeProjectedObjectClient struct {
	object           *unstructured.Unstructured
	getErr           error
	createErr        error
	createSideEffect bool
	raceObject       *unstructured.Unstructured
	getCalls         int
	createCalls      int
}

func (f *fakeProjectedObjectClient) Get(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.object == nil {
		return nil, apierrors.NewNotFound(
			schema.GroupResource{Group: expected.GroupVersionKind().Group, Resource: expected.GetKind()},
			expected.GetName(),
		)
	}
	return f.object.DeepCopy(), nil
}

func (f *fakeProjectedObjectClient) Create(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	f.createCalls++
	if f.raceObject != nil {
		f.object = f.raceObject.DeepCopy()
		return nil, apierrors.NewAlreadyExists(
			schema.GroupResource{Group: expected.GroupVersionKind().Group, Resource: expected.GetKind()},
			expected.GetName(),
		)
	}
	if f.createSideEffect {
		f.object = expected.DeepCopy()
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.object = expected.DeepCopy()
	return f.object.DeepCopy(), nil
}

func TestEnsureProjectedObjectCreatesOnceThenAdopts(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeProjectedObjectClient{}

	action, err := ensureProjectedObject(context.Background(), client, expected)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectCreate || client.createCalls != 1 {
		t.Fatalf("first reconcile: action=%q creates=%d", action, client.createCalls)
	}

	action, err = ensureProjectedObject(context.Background(), client, expected)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectAdopt || client.createCalls != 1 {
		t.Fatalf("replay: action=%q creates=%d", action, client.createCalls)
	}
}

func TestEnsureProjectedObjectLostCreateAckReplaysByAdoption(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeProjectedObjectClient{
		createSideEffect: true,
		createErr:        context.DeadlineExceeded,
	}

	if _, err := ensureProjectedObject(context.Background(), client, expected); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost ACK error=%v, want deadline exceeded", err)
	}
	if client.object == nil || client.createCalls != 1 {
		t.Fatalf("remote create side effect was not simulated: object=%v creates=%d", client.object != nil, client.createCalls)
	}

	action, err := ensureProjectedObject(context.Background(), client, expected)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectAdopt || client.createCalls != 1 {
		t.Fatalf("lost-ACK replay duplicated create: action=%q creates=%d", action, client.createCalls)
	}
}

func TestEnsureProjectedObjectAlreadyExistsRaceAdoptsOnlyExactProjection(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeProjectedObjectClient{raceObject: expected.DeepCopy()}

	action, err := ensureProjectedObject(context.Background(), client, expected)
	if err != nil {
		t.Fatal(err)
	}
	if action != existingObjectAdopt || client.createCalls != 1 || client.getCalls != 2 {
		t.Fatalf("race recovery: action=%q gets=%d creates=%d", action, client.getCalls, client.createCalls)
	}
}

func TestEnsureProjectedObjectAlreadyExistsRaceRejectsGenerationConflict(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	conflict := expected.DeepCopy()
	annotations := conflict.GetAnnotations()
	annotations[generationAnnotation] = "6"
	conflict.SetAnnotations(annotations)
	client := &fakeProjectedObjectClient{raceObject: conflict}

	if _, err := ensureProjectedObject(context.Background(), client, expected); err == nil {
		t.Fatal("AlreadyExists race with another generation must fail closed")
	}
	if client.createCalls != 1 || client.getCalls != 2 {
		t.Fatalf("unexpected race call counts: gets=%d creates=%d", client.getCalls, client.createCalls)
	}
}

func TestEnsureProjectedObjectExistingSpecDriftNeverCreates(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	conflict := expected.DeepCopy()
	if err := unstructured.SetNestedField(conflict.Object, "vq-other", "spec", "queue"); err != nil {
		t.Fatal(err)
	}
	client := &fakeProjectedObjectClient{object: conflict}

	if _, err := ensureProjectedObject(context.Background(), client, expected); err == nil {
		t.Fatal("same-name immutable spec drift must fail closed")
	}
	if client.createCalls != 0 {
		t.Fatalf("conflicting existing object triggered create: %d", client.createCalls)
	}
}

func TestEnsureProjectedObjectGetFailureDoesNotCreateBlindly(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeProjectedObjectClient{getErr: context.DeadlineExceeded}

	if _, err := ensureProjectedObject(context.Background(), client, expected); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("get error=%v, want deadline exceeded", err)
	}
	if client.createCalls != 0 {
		t.Fatalf("ambiguous GET must not trigger create: %d", client.createCalls)
	}
}
