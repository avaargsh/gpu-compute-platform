package volcano

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func fixedProvider(t *testing.T) (*Provider, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	client := fakeVolcanoDynamicClient()
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	provider.now = func() time.Time {
		return time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	}
	return provider, client
}

func TestVolcanoProviderReconcilePoolCreatesThenAdopts(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionPoolProjection()

	first, err := provider.ReconcilePool(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if first.ObservedGeneration != projection.Generation ||
		len(first.Conditions) != 1 ||
		first.Conditions[0].Status != "False" ||
		first.Conditions[0].Reason != "AwaitingVolcanoQueueState" {
		t.Fatalf("new Queue must wait for controller Open state: %#v", first)
	}

	expected, err := ProjectPool(projection)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := client.Resource(volcanoQueueGVR).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	queue.Object["status"] = map[string]any{"state": "Open"}
	if _, err := client.Resource(volcanoQueueGVR).
		Update(context.Background(), queue, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	replayed, err := provider.ReconcilePool(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Conditions[0].Status != "True" ||
		replayed.Conditions[0].Reason != "VolcanoQueueOpen" {
		t.Fatalf("Open Queue did not become Ready: %#v", replayed.Conditions)
	}
}

func TestVolcanoQueueNonOpenStatesFailClosed(t *testing.T) {
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	for _, state := range []string{"Pending", "Closed", "Closing", "Unknown", "Unexpected"} {
		condition := volcanoQueueReadyCondition(state, existingObjectAdopt, now)
		if condition.Status != "False" {
			t.Fatalf("Queue state %q unexpectedly Ready: %#v", state, condition)
		}
	}
}

func TestVolcanoProviderReconcileWorkloadObservesFreshControllerPhase(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionWorkloadProjection()

	first, err := provider.ReconcileWorkload(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if first.Phase != "Pending" || first.Conditions[0].Status != "False" {
		t.Fatalf("new VolcanoJob should be pending: %#v", first)
	}

	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}
	current, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current.Object["status"] = map[string]any{
		"state": map[string]any{"phase": "Running"},
	}
	if _, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	running, err := provider.ReconcileWorkload(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if running.Phase != "Running" ||
		running.Conditions[0].Status != "True" ||
		running.Conditions[0].Reason != "VolcanoJobRunning" {
		t.Fatalf("unexpected running observation: %#v", running)
	}

	current, err = client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current.Object["status"] = map[string]any{
		"state": map[string]any{"phase": "Completed"},
	}
	if _, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	completed, err := provider.ReconcileWorkload(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Phase != "Completed" || len(completed.Conditions) != 2 {
		t.Fatalf("unexpected completed observation: %#v", completed)
	}
	if completed.Conditions[1].Type != "Succeeded" ||
		completed.Conditions[1].Status != "True" {
		t.Fatalf("completed VolcanoJob did not surface success: %#v", completed.Conditions)
	}
}

func TestVolcanoProviderSurfacesTerminalFailurePhases(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionWorkloadProjection()
	if _, err := provider.ReconcileWorkload(context.Background(), projection); err != nil {
		t.Fatal(err)
	}
	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}

	for _, phase := range []string{"Failed", "Aborted", "Terminated"} {
		current, err := client.Resource(volcanoJobGVR).
			Namespace(expected.GetNamespace()).
			Get(context.Background(), expected.GetName(), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		current.Object["status"] = map[string]any{
			"state": map[string]any{"phase": phase},
		}
		if _, err := client.Resource(volcanoJobGVR).
			Namespace(expected.GetNamespace()).
			Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}

		observed, err := provider.ReconcileWorkload(context.Background(), projection)
		if err != nil {
			t.Fatal(err)
		}
		if len(observed.Conditions) != 2 ||
			observed.Conditions[0].Status != "False" ||
			observed.Conditions[1].Type != "Failed" ||
			observed.Conditions[1].Status != "True" {
			t.Fatalf("phase %q did not surface terminal failure: %#v", phase, observed)
		}
	}
}

func TestVolcanoProviderRejectsStaleObservedOwnershipBeforeReporting(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionWorkloadProjection()
	if _, err := provider.ReconcileWorkload(context.Background(), projection); err != nil {
		t.Fatal(err)
	}

	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}
	current, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	annotations := current.GetAnnotations()
	annotations[generationAnnotation] = "6"
	current.SetAnnotations(annotations)
	if _, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := provider.ReconcileWorkload(context.Background(), projection); err == nil {
		t.Fatal("stale provider generation must not be reported as observed")
	}
}

func TestVolcanoProviderDeleteWorkloadVerifiesOwnershipBeforeSideEffect(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionWorkloadProjection()
	if _, err := provider.ReconcileWorkload(context.Background(), projection); err != nil {
		t.Fatal(err)
	}

	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}
	current, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	annotations := current.GetAnnotations()
	annotations[workloadIDAnnotation] = "foreign-workload"
	current.SetAnnotations(annotations)
	if _, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := provider.DeleteWorkload(context.Background(), projection); err == nil {
		t.Fatal("delete must refuse conflicting ownership")
	}
	if _, err := client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("conflicting object was deleted: %v", err)
	}
}

func TestVolcanoProviderDeleteWorkloadIsIdempotentAndConfirmsGone(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionWorkloadProjection()
	if _, err := provider.ReconcileWorkload(context.Background(), projection); err != nil {
		t.Fatal(err)
	}
	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}

	first, err := provider.DeleteWorkload(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Gone {
		t.Fatalf("delete did not confirm disappearance: %#v", first)
	}
	_, err = client.Resource(volcanoJobGVR).
		Namespace(expected.GetNamespace()).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("VolcanoJob still exists after confirmed delete: %v", err)
	}

	second, err := provider.DeleteWorkload(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Gone {
		t.Fatalf("replayed delete must remain gone: %#v", second)
	}
}

func TestVolcanoProviderDeletePoolUsesSameOwnershipFence(t *testing.T) {
	provider, client := fixedProvider(t)
	projection := adoptionPoolProjection()
	if _, err := provider.ReconcilePool(context.Background(), projection); err != nil {
		t.Fatal(err)
	}
	expected, err := ProjectPool(projection)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := provider.DeletePool(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Gone {
		t.Fatalf("Queue delete did not converge: %#v", deleted)
	}
	_, err = client.Resource(volcanoQueueGVR).
		Get(context.Background(), expected.GetName(), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Volcano Queue still exists after delete: %v", err)
	}
}

func TestVolcanoProviderMarksTransientKubernetesFailuresRetryable(t *testing.T) {
	client := &failingVolcanoObjectClient{getErr: context.DeadlineExceeded}
	provider := newProvider(client)

	_, err := provider.ReconcilePool(context.Background(), adoptionPoolProjection())
	if err == nil || !baseprovider.IsRetryable(err) {
		t.Fatalf("transient provider error was not marked retryable: %v", err)
	}
}

type failingVolcanoObjectClient struct {
	getErr error
}

func (f *failingVolcanoObjectClient) Get(
	context.Context,
	*unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	return nil, f.getErr
}

func (f *failingVolcanoObjectClient) Create(
	context.Context,
	*unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	return nil, context.DeadlineExceeded
}

func (f *failingVolcanoObjectClient) Update(
	context.Context,
	*unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	return nil, context.DeadlineExceeded
}

func (f *failingVolcanoObjectClient) Delete(
	context.Context,
	*unstructured.Unstructured,
) error {
	return context.DeadlineExceeded
}

func TestVolcanoProviderFreshObservationNotFoundIsRetryable(t *testing.T) {
	client := &vanishingAfterCreateClient{}
	provider := newProvider(client)

	_, err := provider.ReconcilePool(context.Background(), adoptionPoolProjection())
	if err == nil || !baseprovider.IsRetryable(err) {
		t.Fatalf("post-ensure disappearance must be retryable: %v", err)
	}
	if client.createCalls != 1 || client.getCalls != 2 {
		t.Fatalf("unexpected calls: gets=%d creates=%d", client.getCalls, client.createCalls)
	}
}

func TestVolcanoProviderDeleteDetectsConflictingRecreateBeforeFinalization(t *testing.T) {
	expected, err := ProjectWorkload(adoptionWorkloadProjection())
	if err != nil {
		t.Fatal(err)
	}
	conflict := expected.DeepCopy()
	annotations := conflict.GetAnnotations()
	annotations[generationAnnotation] = "999"
	conflict.SetAnnotations(annotations)

	client := &replaceOnDeleteClient{
		object:      expected.DeepCopy(),
		replacement: conflict,
	}
	provider := newProvider(client)

	deleted, err := provider.DeleteWorkload(context.Background(), adoptionWorkloadProjection())
	if err == nil {
		t.Fatalf("conflicting recreate must block finalization: %#v", deleted)
	}
	if deleted.Gone {
		t.Fatal("conflicting recreated object must never be reported gone")
	}
	if client.deleteCalls != 1 {
		t.Fatalf("delete calls=%d, want 1", client.deleteCalls)
	}
}

func TestVolcanoReadyConditionUnknownAndFailedPhasesFailClosed(t *testing.T) {
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	for _, phase := range []string{"Mystery", "Failed", "Aborted", "Terminated"} {
		condition := volcanoReadyCondition(phase, now)
		if condition.Status != "False" {
			t.Fatalf("phase %q unexpectedly Ready: %#v", phase, condition)
		}
	}
}

type vanishingAfterCreateClient struct {
	getCalls    int
	createCalls int
}

func (c *vanishingAfterCreateClient) Get(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.getCalls++
	return nil, apierrors.NewNotFound(
		schema.GroupResource{
			Group:    expected.GroupVersionKind().Group,
			Resource: expected.GetKind(),
		},
		expected.GetName(),
	)
}

func (c *vanishingAfterCreateClient) Create(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.createCalls++
	return expected.DeepCopy(), nil
}

func (c *vanishingAfterCreateClient) Update(
	context.Context,
	*unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	return nil, context.DeadlineExceeded
}

func (c *vanishingAfterCreateClient) Delete(
	context.Context,
	*unstructured.Unstructured,
) error {
	return nil
}

type replaceOnDeleteClient struct {
	object      *unstructured.Unstructured
	replacement *unstructured.Unstructured
	deleteCalls int
}

func (c *replaceOnDeleteClient) Get(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	if c.object == nil {
		return nil, apierrors.NewNotFound(
			schema.GroupResource{
				Group:    expected.GroupVersionKind().Group,
				Resource: expected.GetKind(),
			},
			expected.GetName(),
		)
	}
	return c.object.DeepCopy(), nil
}

func (c *replaceOnDeleteClient) Create(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.object = expected.DeepCopy()
	return c.object.DeepCopy(), nil
}

func (c *replaceOnDeleteClient) Update(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.object = expected.DeepCopy()
	return c.object.DeepCopy(), nil
}

func (c *replaceOnDeleteClient) Delete(
	_ context.Context,
	_ *unstructured.Unstructured,
) error {
	c.deleteCalls++
	c.object = c.replacement.DeepCopy()
	return nil
}


func TestVolcanoProviderUpdatesOlderQueueGenerationWithCASIdentity(t *testing.T) {
	provider, client := fixedProvider(t)
	v4 := adoptionPoolProjection()
	if _, err := provider.ReconcilePool(context.Background(), v4); err != nil {
		t.Fatal(err)
	}
	expectedV4, err := ProjectPool(v4)
	if err != nil {
		t.Fatal(err)
	}
	current, err := client.Resource(volcanoQueueGVR).
		Get(context.Background(), expectedV4.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	annotations := current.GetAnnotations()
	annotations["volcano.sh/controller-note"] = "keep-me"
	current.SetAnnotations(annotations)
	current.SetFinalizers([]string{"volcano.sh/protect"})
	current.Object["status"] = map[string]any{"state": "Open"}
	if _, err := client.Resource(volcanoQueueGVR).
		Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	v5 := v4
	v5.Generation = 5
	v5.Accelerators = []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 16}}
	observed, err := provider.ReconcilePool(context.Background(), v5)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ObservedGeneration != 5 {
		t.Fatalf("observed generation=%d, want 5", observed.ObservedGeneration)
	}

	expectedV5, err := ProjectPool(v5)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := client.Resource(volcanoQueueGVR).
		Get(context.Background(), expectedV5.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[generationAnnotation] != "5" {
		t.Fatalf("Queue generation annotation=%q, want 5", updated.GetAnnotations()[generationAnnotation])
	}
	capability, _, _ := unstructured.NestedStringMap(updated.Object, "spec", "capability")
	if capability["nvidia.com/gpu"] != "16" {
		t.Fatalf("Queue capability=%#v, want nvidia.com/gpu=16", capability)
	}
	if updated.GetAnnotations()["volcano.sh/controller-note"] != "keep-me" {
		t.Fatalf("non-platform annotation was lost: %#v", updated.GetAnnotations())
	}
	if len(updated.GetFinalizers()) != 1 || updated.GetFinalizers()[0] != "volcano.sh/protect" {
		t.Fatalf("controller finalizer was lost: %#v", updated.GetFinalizers())
	}
}

func TestVolcanoProviderRejectsStaleOrSameGenerationQueueMutation(t *testing.T) {
	provider, _ := fixedProvider(t)
	v4 := adoptionPoolProjection()
	if _, err := provider.ReconcilePool(context.Background(), v4); err != nil {
		t.Fatal(err)
	}
	v5 := v4
	v5.Generation = 5
	v5.Accelerators = []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 16}}
	if _, err := provider.ReconcilePool(context.Background(), v5); err != nil {
		t.Fatal(err)
	}

	if _, err := provider.ReconcilePool(context.Background(), v4); err == nil {
		t.Fatal("older desired generation must not roll Queue back")
	}

	sameGenerationMutation := v5
	sameGenerationMutation.Accelerators = []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 32}}
	if _, err := provider.ReconcilePool(context.Background(), sameGenerationMutation); err == nil {
		t.Fatal("same-generation Queue spec drift must fail closed")
	}
}

func TestVolcanoProviderLostQueueUpdateAckAdoptsCommittedGeneration(t *testing.T) {
	v4 := adoptionPoolProjection()
	current, err := ProjectPool(v4)
	if err != nil {
		t.Fatal(err)
	}
	v5 := v4
	v5.Generation = 5
	v5.Accelerators = []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 16}}

	client := &lostUpdateAckClient{
		object: current.DeepCopy(),
	}
	provider := newProvider(client)

	_, err = provider.ReconcilePool(context.Background(), v5)
	if err == nil || !baseprovider.IsRetryable(err) {
		t.Fatalf("lost Queue update ACK must be retryable: %v", err)
	}
	if client.updateCalls != 1 {
		t.Fatalf("update calls=%d, want 1", client.updateCalls)
	}

	observed, err := provider.ReconcilePool(context.Background(), v5)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ObservedGeneration != 5 {
		t.Fatalf("replayed observation generation=%d, want 5", observed.ObservedGeneration)
	}
	if client.updateCalls != 1 {
		t.Fatalf("lost-ACK replay issued duplicate Queue update: %d", client.updateCalls)
	}
}

type lostUpdateAckClient struct {
	object      *unstructured.Unstructured
	updateCalls int
}

func (c *lostUpdateAckClient) Get(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	if c.object == nil {
		return nil, apierrors.NewNotFound(
			schema.GroupResource{
				Group:    expected.GroupVersionKind().Group,
				Resource: expected.GetKind(),
			},
			expected.GetName(),
		)
	}
	return c.object.DeepCopy(), nil
}

func (c *lostUpdateAckClient) Create(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.object = expected.DeepCopy()
	return c.object.DeepCopy(), nil
}

func (c *lostUpdateAckClient) Update(
	_ context.Context,
	expected *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	c.updateCalls++
	c.object = expected.DeepCopy()
	return nil, context.DeadlineExceeded
}

func (c *lostUpdateAckClient) Delete(
	context.Context,
	*unstructured.Unstructured,
) error {
	c.object = nil
	return nil
}
