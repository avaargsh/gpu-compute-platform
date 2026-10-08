package volcano

import (
	"context"
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

// Kubernetes rejects a stale resourceVersion at DELETE time. The adapter must
// surface a retryable error, then start over with GET and identity validation;
// it must not keep retrying the previously authorized DELETE.
func TestVolcanoDeleteConflictReplaysThroughFreshRead(t *testing.T) {
	projection := adoptionWorkloadProjection()
	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{
		object: expected.DeepCopy(),
		deleteErr: apierrors.NewConflict(
			schema.GroupResource{Group: volcanoJobGVR.Group, Resource: volcanoJobGVR.Resource},
			expected.GetName(),
			errors.New("resourceVersion changed"),
		),
	}
	adapter := newProvider(client)

	first, err := adapter.DeleteWorkload(context.Background(), projection)
	if err == nil || !baseprovider.IsRetryable(err) || !apierrors.IsConflict(err) {
		t.Fatalf("DELETE conflict must be preserved as retryable: observation=%#v err=%v", first, err)
	}
	if first.Gone || client.deleteCalls != 1 || client.object == nil {
		t.Fatalf("conflict must not report deletion or mutate object: observation=%#v deletes=%d", first, client.deleteCalls)
	}

	// Simulate a later Agent retry after the conflict; this is a separate
	// reconciliation and must read the object again before deleting.
	client.deleteErr = nil
	client.deleteSideEffect = true
	second, err := adapter.DeleteWorkload(context.Background(), projection)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Gone || client.object != nil || client.deleteCalls != 2 {
		t.Fatalf("replay failed to converge: observation=%#v deletes=%d", second, client.deleteCalls)
	}
}

func TestVolcanoDeleteConflictNeverWaivesOwnershipOnReplay(t *testing.T) {
	projection := adoptionWorkloadProjection()
	expected, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeLifecycleClient{
		object: expected.DeepCopy(),
		deleteErr: apierrors.NewConflict(
			schema.GroupResource{Group: volcanoJobGVR.Group, Resource: volcanoJobGVR.Resource},
			expected.GetName(), errors.New("concurrent update"),
		),
	}
	adapter := newProvider(client)
	if _, err := adapter.DeleteWorkload(context.Background(), projection); err == nil || !baseprovider.IsRetryable(err) {
		t.Fatalf("initial conflict must retry: %v", err)
	}

	// Between attempts a foreign actor takes over the deterministic name.
	foreign := expected.DeepCopy()
	annotations := foreign.GetAnnotations()
	annotations[workloadIDAnnotation] = "foreign-workload"
	foreign.SetAnnotations(annotations)
	client.object = foreign
	client.deleteErr = nil
	client.deleteSideEffect = true

	observation, err := adapter.DeleteWorkload(context.Background(), projection)
	if err == nil || baseprovider.IsRetryable(err) || observation.Gone {
		t.Fatalf("foreign takeover must fail closed: observation=%#v err=%v", observation, err)
	}
	if client.deleteCalls != 1 || client.object == nil {
		t.Fatalf("foreign object was deleted on retry: deletes=%d", client.deleteCalls)
	}
}

func TestVolcanoConflictClassificationDoesNotRetryPermanentFailures(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{"conflict", apierrors.NewConflict(
			schema.GroupResource{Group: "batch.volcano.sh", Resource: "jobs"}, "job",
			errors.New("resourceVersion mismatch")), true},
		{"forbidden", apierrors.NewForbidden(
			schema.GroupResource{Group: "batch.volcano.sh", Resource: "jobs"}, "job",
			errors.New("RBAC denied")), false},
		{"canceled", context.Canceled, false},
		{"ownership", fmt.Errorf("provider object conflict: foreign owner"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := classifyProviderError(tt.err)
			if got := baseprovider.IsRetryable(classified); got != tt.wantRetry {
				t.Fatalf("retryable=%t, want %t, err=%v", got, tt.wantRetry, classified)
			}
		})
	}
}
