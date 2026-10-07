package volcano

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

var _ baseprovider.Adapter = (*Provider)(nil)

type volcanoObjectClient interface {
	projectedObjectClient
	Update(context.Context, *unstructured.Unstructured) (*unstructured.Unstructured, error)
	Delete(context.Context, *unstructured.Unstructured) error
}

const existingObjectUpdate existingObjectAction = "update"

type Provider struct {
	client volcanoObjectClient
	now    func() time.Time
}

// NewProvider constructs the Stage B Volcano adapter logic, but production
// Cluster Runtime intentionally does not register it yet.
func NewProvider(client dynamic.Interface) (*Provider, error) {
	transport, err := newDynamicProjectedObjectClient(client)
	if err != nil {
		return nil, err
	}
	return newProvider(transport), nil
}

func newProvider(client volcanoObjectClient) *Provider {
	return &Provider{
		client: client,
		now:    time.Now,
	}
}

func (p *Provider) ReconcilePool(
	ctx context.Context,
	projection baseprovider.PoolProjection,
) (baseprovider.PoolObservation, error) {
	if p == nil || p.client == nil {
		return baseprovider.PoolObservation{}, fmt.Errorf("Volcano client is required")
	}
	expected, err := ProjectPool(projection)
	if err != nil {
		return baseprovider.PoolObservation{}, err
	}
	action, err := p.ensurePoolQueue(ctx, expected)
	if err != nil {
		return baseprovider.PoolObservation{}, fmt.Errorf("ensure Volcano Queue: %w", classifyProviderError(err))
	}
	current, err := p.observeOwnedObject(ctx, expected, "Volcano Queue")
	if err != nil {
		return baseprovider.PoolObservation{}, err
	}
	state, err := volcanoQueueState(current)
	if err != nil {
		return baseprovider.PoolObservation{}, err
	}

	return baseprovider.PoolObservation{
		ObservedGeneration: projection.Generation,
		Conditions: []domain.Condition{
			volcanoQueueReadyCondition(state, action, p.now().UTC()),
		},
		EvidenceRefs: []string{
			fmt.Sprintf("volcano://%s/queues/%s", projection.ClusterID, expected.GetName()),
		},
	}, nil
}

func (p *Provider) ensurePoolQueue(
	ctx context.Context,
	expected *unstructured.Unstructured,
) (existingObjectAction, error) {
	if _, err := classifyExistingObject(expected, nil); err != nil {
		return "", err
	}

	current, err := p.client.Get(ctx, expected)
	if apierrors.IsNotFound(err) {
		return ensureProjectedObject(ctx, p.client, expected)
	}
	if err != nil {
		return "", err
	}

	wantGeneration, haveGeneration, err := validateQueueUpdateIdentity(expected, current)
	if err != nil {
		return "", err
	}
	switch {
	case haveGeneration == wantGeneration:
		action, err := classifyExistingObject(expected, current)
		if err != nil {
			return "", err
		}
		return action, nil
	case haveGeneration > wantGeneration:
		return "", providerObjectConflict(
			"Queue generation is newer than desired: have %d want %d",
			haveGeneration,
			wantGeneration,
		)
	}

	candidate := queueUpdateCandidate(expected, current)
	updated, err := p.client.Update(ctx, candidate)
	if err != nil {
		return "", err
	}
	if _, err := classifyExistingObject(expected, updated); err != nil {
		return "", fmt.Errorf("updated Volcano Queue violates desired projection: %w", err)
	}
	return existingObjectUpdate, nil
}

func validateQueueUpdateIdentity(
	expected *unstructured.Unstructured,
	current *unstructured.Unstructured,
) (int64, int64, error) {
	if current == nil {
		return 0, 0, providerObjectConflict("existing Queue is required for update")
	}
	if expected.GetAPIVersion() != current.GetAPIVersion() ||
		expected.GetKind() != "Queue" ||
		current.GetKind() != "Queue" ||
		expected.GetName() != current.GetName() ||
		expected.GetNamespace() != current.GetNamespace() {
		return 0, 0, providerObjectConflict("Queue GVK/name identity mismatch")
	}

	wantAnnotations := expected.GetAnnotations()
	haveAnnotations := current.GetAnnotations()
	for _, key := range []string{
		providerAnnotation,
		poolIDAnnotation,
		acceleratorClassAnnotation,
	} {
		if wantAnnotations[key] == "" || haveAnnotations[key] != wantAnnotations[key] {
			return 0, 0, providerObjectConflict(
				"Queue annotation %s mismatch: have %q want %q",
				key,
				haveAnnotations[key],
				wantAnnotations[key],
			)
		}
	}

	wantGeneration, err := strconv.ParseInt(wantAnnotations[generationAnnotation], 10, 64)
	if err != nil || wantGeneration <= 0 {
		return 0, 0, fmt.Errorf("invalid desired Queue generation %q", wantAnnotations[generationAnnotation])
	}
	haveGeneration, err := strconv.ParseInt(haveAnnotations[generationAnnotation], 10, 64)
	if err != nil || haveGeneration <= 0 {
		return 0, 0, providerObjectConflict(
			"invalid existing Queue generation %q",
			haveAnnotations[generationAnnotation],
		)
	}
	return wantGeneration, haveGeneration, nil
}

func queueUpdateCandidate(
	expected *unstructured.Unstructured,
	current *unstructured.Unstructured,
) *unstructured.Unstructured {
	candidate := expected.DeepCopy()
	candidate.SetResourceVersion(current.GetResourceVersion())
	candidate.SetUID(current.GetUID())
	candidate.SetFinalizers(append([]string(nil), current.GetFinalizers()...))
	candidate.SetOwnerReferences(append([]metav1.OwnerReference(nil), current.GetOwnerReferences()...))

	annotations := cloneProviderMetadata(current.GetAnnotations(), expected.GetAnnotations())
	candidate.SetAnnotations(annotations)
	labels := cloneProviderMetadata(current.GetLabels(), expected.GetLabels())
	candidate.SetLabels(labels)
	return candidate
}

func cloneProviderMetadata(existing, desired map[string]string) map[string]string {
	out := make(map[string]string, len(existing)+len(desired))
	for key, value := range existing {
		if strings.HasPrefix(key, "ai.compute/") {
			continue
		}
		out[key] = value
	}
	for key, value := range desired {
		out[key] = value
	}
	return out
}

func (p *Provider) ReconcileWorkload(
	ctx context.Context,
	projection baseprovider.WorkloadProjection,
) (baseprovider.WorkloadObservation, error) {
	if p == nil || p.client == nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("Volcano client is required")
	}
	expected, err := ProjectWorkload(projection)
	if err != nil {
		return baseprovider.WorkloadObservation{}, err
	}
	if _, err := ensureProjectedObject(ctx, p.client, expected); err != nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("ensure VolcanoJob: %w", classifyProviderError(err))
	}

	// Observation is a fresh GET after ensure. Do not report the desired
	// generation from a stale in-memory create response.
	current, err := p.observeOwnedObject(ctx, expected, "VolcanoJob")
	if err != nil {
		return baseprovider.WorkloadObservation{}, err
	}

	phase, err := volcanoJobPhase(current)
	if err != nil {
		return baseprovider.WorkloadObservation{}, err
	}
	now := p.now().UTC()
	conditions := []domain.Condition{volcanoReadyCondition(phase, now)}
	switch phase {
	case "Completed":
		conditions = append(conditions, domain.Condition{
			Type:               "Succeeded",
			Status:             "True",
			Reason:             "VolcanoJobCompleted",
			LastTransitionTime: now,
		})
	case "Failed", "Aborted", "Terminated":
		conditions = append(conditions, domain.Condition{
			Type:               "Failed",
			Status:             "True",
			Reason:             "VolcanoJob" + phase,
			LastTransitionTime: now,
		})
	}

	return baseprovider.WorkloadObservation{
		ObservedGeneration: projection.Generation,
		Phase:              phase,
		Conditions:         conditions,
		EvidenceRefs: []string{
			fmt.Sprintf(
				"volcano://%s/namespaces/%s/jobs/%s",
				projection.ClusterID,
				expected.GetNamespace(),
				expected.GetName(),
			),
		},
	}, nil
}

func (p *Provider) observeOwnedObject(
	ctx context.Context,
	expected *unstructured.Unstructured,
	description string,
) (*unstructured.Unstructured, error) {
	current, err := p.client.Get(ctx, expected)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf(
				"observe %s after ensure: %w",
				description,
				baseprovider.MarkRetryable(err),
			)
		}
		return nil, fmt.Errorf("observe %s: %w", description, classifyProviderError(err))
	}
	if _, err := classifyExistingObject(expected, current); err != nil {
		return nil, fmt.Errorf("observe %s ownership: %w", description, err)
	}
	return current, nil
}

func (p *Provider) DeletePool(
	ctx context.Context,
	projection baseprovider.PoolProjection,
) (baseprovider.DeletionObservation, error) {
	if p == nil || p.client == nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("Volcano client is required")
	}
	expected, err := ProjectPool(projection)
	if err != nil {
		return baseprovider.DeletionObservation{}, err
	}
	return p.deleteOwnedObject(
		ctx,
		expected,
		fmt.Sprintf("volcano://%s/queues/%s", projection.ClusterID, expected.GetName()),
	)
}

func (p *Provider) DeleteWorkload(
	ctx context.Context,
	projection baseprovider.WorkloadProjection,
) (baseprovider.DeletionObservation, error) {
	if p == nil || p.client == nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("Volcano client is required")
	}
	expected, err := ProjectWorkload(projection)
	if err != nil {
		return baseprovider.DeletionObservation{}, err
	}
	return p.deleteOwnedObject(
		ctx,
		expected,
		fmt.Sprintf(
			"volcano://%s/namespaces/%s/jobs/%s",
			projection.ClusterID,
			expected.GetNamespace(),
			expected.GetName(),
		),
	)
}

func (p *Provider) deleteOwnedObject(
	ctx context.Context,
	expected *unstructured.Unstructured,
	evidence string,
) (baseprovider.DeletionObservation, error) {
	if _, err := classifyExistingObject(expected, nil); err != nil {
		return baseprovider.DeletionObservation{}, err
	}

	current, err := p.client.Get(ctx, expected)
	if apierrors.IsNotFound(err) {
		return baseprovider.DeletionObservation{Gone: true, EvidenceRefs: []string{evidence}}, nil
	}
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("get Volcano object before delete: %w", classifyProviderError(err))
	}
	if _, err := classifyExistingObject(expected, current); err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("refuse to delete unowned/conflicting Volcano object: %w", err)
	}

	if err := p.client.Delete(ctx, expected); err != nil && !apierrors.IsNotFound(err) {
		return baseprovider.DeletionObservation{}, fmt.Errorf("delete Volcano object: %w", classifyProviderError(err))
	}

	current, err = p.client.Get(ctx, expected)
	if apierrors.IsNotFound(err) {
		return baseprovider.DeletionObservation{Gone: true, EvidenceRefs: []string{evidence}}, nil
	}
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("verify Volcano object deletion: %w", classifyProviderError(err))
	}
	if _, err := classifyExistingObject(expected, current); err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("object identity changed during Volcano deletion: %w", err)
	}
	return baseprovider.DeletionObservation{Gone: false, EvidenceRefs: []string{evidence}}, nil
}

func volcanoQueueState(queue *unstructured.Unstructured) (string, error) {
	state, found, err := unstructured.NestedString(queue.Object, "status", "state")
	if err != nil {
		return "", fmt.Errorf("read Volcano Queue status.state: %w", err)
	}
	if !found || strings.TrimSpace(state) == "" {
		return "Pending", nil
	}
	return state, nil
}

func volcanoQueueReadyCondition(
	state string,
	action existingObjectAction,
	now time.Time,
) domain.Condition {
	condition := domain.Condition{
		Type:               "Ready",
		Status:             "False",
		Reason:             "VolcanoQueue" + state,
		Message:            "Volcano Queue is not open for admission",
		LastTransitionTime: now,
	}
	if state == "Open" {
		condition.Status = "True"
		condition.Reason = "VolcanoQueueOpen"
		condition.Message = "Volcano Queue is open and matches the frozen provider projection"
		return condition
	}
	if state == "Pending" {
		condition.Reason = "AwaitingVolcanoQueueState"
		condition.Message = "Volcano Queue was " + string(action) + " but controller status is not observed yet"
	}
	return condition
}

func volcanoJobPhase(job *unstructured.Unstructured) (string, error) {
	phase, found, err := unstructured.NestedString(job.Object, "status", "state", "phase")
	if err != nil {
		return "", fmt.Errorf("read VolcanoJob status.state.phase: %w", err)
	}
	if !found || strings.TrimSpace(phase) == "" {
		return "Pending", nil
	}
	switch phase {
	case "Pending", "Running", "Restarting", "Completing", "Completed",
		"Aborting", "Aborted", "Terminating", "Terminated", "Failed":
		return phase, nil
	default:
		// Preserve an unknown controller phase for visibility, but do not
		// accidentally classify it as Ready or Succeeded.
		return phase, nil
	}
}

func volcanoReadyCondition(phase string, now time.Time) domain.Condition {
	condition := domain.Condition{
		Type:               "Ready",
		Status:             "False",
		Reason:             "VolcanoJob" + phase,
		LastTransitionTime: now,
	}
	switch phase {
	case "Running":
		condition.Status = "True"
		condition.Reason = "VolcanoJobRunning"
	case "Completed":
		condition.Status = "True"
		condition.Reason = "VolcanoJobCompleted"
	}
	return condition
}
