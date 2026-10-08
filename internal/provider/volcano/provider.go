package volcano

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

var _ baseprovider.Adapter = (*Provider)(nil)

type volcanoObjectClient interface {
	projectedObjectClient
	Delete(context.Context, *unstructured.Unstructured) error
}

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
	action, updateUID, err := p.ensurePoolQueue(ctx, expected)
	if err != nil {
		return baseprovider.PoolObservation{}, fmt.Errorf("ensure Volcano Queue: %w", classifyProviderError(err))
	}
	current, err := p.observeOwnedObject(ctx, expected, "Volcano Queue")
	if err != nil {
		return baseprovider.PoolObservation{}, err
	}
	// A matching projection alone is not a proof of the UPDATE we just
	// performed. A deleted/recreated Queue with a new UID must fail closed
	// even when its generation, owner annotations and quota are identical.
	if updateUID != "" && string(current.GetUID()) != updateUID {
		return baseprovider.PoolObservation{}, providerObjectConflict(
			"Queue UID changed after successful quota UPDATE: updated %s observed %s",
			updateUID, current.GetUID(),
		)
	}
	state, err := volcanoQueueState(current)
	if err != nil {
		return baseprovider.PoolObservation{}, err
	}

	return baseprovider.PoolObservation{
		ObservedGeneration: projection.Generation,
		Conditions: []domain.Condition{
			volcanoQueueReadyCondition(state, action, p.now().UTC()),
			{
				Type:               "QuotaApplied",
				Status:             "Unknown",
				Reason:             "VolcanoQueueLacksAppliedGenerationEvidence",
				Message:            "Fresh Queue GET confirms stored quota, not scheduler adoption; require independent workload/admission evidence",
				LastTransitionTime: p.now().UTC(),
			},
		},
		EvidenceRefs: []string{
			fmt.Sprintf("volcano://%s/queues/%s", projection.ClusterID, expected.GetName()),
		},
	}, nil
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
	// Centralize the ownership GET, UID/resourceVersion-preconditioned DELETE
	// and independent observation in the previously accepted Stage B primitive.
	// Reimplementing it here would reopen the GET -> name-only DELETE race.
	gone, err := deleteProjectedObject(ctx, p.client, expected)
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("delete Volcano object: %w", classifyProviderError(err))
	}
	return baseprovider.DeletionObservation{Gone: gone, EvidenceRefs: []string{evidence}}, nil
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
		condition.Message = "Volcano Queue is open for admission; this does not establish that the scheduler applied the current quota"
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
		// Running is not proof that every Pod has become Ready. The pod/PodGroup
		// evidence gate is deliberately deferred in this unregistered adapter.
		condition.Reason = "AwaitingVolcanoPodReadinessEvidence"
	case "Completed":
		condition.Status = "True"
		condition.Reason = "VolcanoJobCompleted"
	}
	return condition
}
