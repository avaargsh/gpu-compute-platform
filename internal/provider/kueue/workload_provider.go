package kueue

import (
	"context"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func (p *Provider) ReconcileWorkload(ctx context.Context, projection baseprovider.WorkloadProjection) (baseprovider.WorkloadObservation, error) {
	if p.client == nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("kueue client is required")
	}

	job, err := ProjectWorkload(projection)
	if err != nil {
		return baseprovider.WorkloadObservation{}, err
	}
	if err := p.client.ApplyJob(ctx, job); err != nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("apply job: %w", err)
	}

	state, err := p.client.ObserveJob(ctx, job.Namespace, job.Name)
	if err != nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("observe job: %w", err)
	}

	now := time.Now().UTC()
	conditions := []domain.Condition{
		{
			Type:               "Ready",
			Status:             boolStatus(state.PodsReady && !state.Failed),
			Reason:             readyReason(state),
			Message:            state.Message,
			LastTransitionTime: now,
		},
		{
			Type:               "QuotaReserved",
			Status:             boolStatus(state.QuotaReserved),
			Reason:             conditionReason(state.QuotaReserved, "KueueQuotaReserved", "AwaitingQuota"),
			LastTransitionTime: now,
		},
		{
			Type:               "Admitted",
			Status:             boolStatus(state.Admitted),
			Reason:             conditionReason(state.Admitted, "KueueAdmitted", "AwaitingAdmission"),
			LastTransitionTime: now,
		},
		{
			Type:               "PodsReady",
			Status:             boolStatus(state.PodsReady),
			Reason:             conditionReason(state.PodsReady, "PodsReady", "AwaitingPods"),
			LastTransitionTime: now,
		},
	}
	if state.Failed {
		conditions = append(conditions, domain.Condition{
			Type:               "Failed",
			Status:             "True",
			Reason:             "JobFailed",
			Message:            state.Message,
			LastTransitionTime: now,
		})
	}

	evidenceRefs := []string{
		fmt.Sprintf("k8s://%s/namespaces/%s/jobs/%s", projection.ClusterID, job.Namespace, job.Name),
	}
	if state.WorkloadName != "" {
		evidenceRefs = append(evidenceRefs,
			fmt.Sprintf("kueue://%s/namespaces/%s/workloads/%s", projection.ClusterID, job.Namespace, state.WorkloadName),
		)
	}

	return baseprovider.WorkloadObservation{
		ObservedGeneration: projection.Generation,
		Phase:              state.Phase,
		Conditions:         conditions,
		EvidenceRefs:       evidenceRefs,
	}, nil
}

func boolStatus(value bool) string {
	if value {
		return "True"
	}
	return "False"
}

func conditionReason(value bool, trueReason, falseReason string) string {
	if value {
		return trueReason
	}
	return falseReason
}

func readyReason(state JobObservation) string {
	switch {
	case state.Failed:
		return "JobFailed"
	case state.PodsReady:
		return "PodsReady"
	case state.Admitted:
		return "AwaitingPods"
	case state.QuotaReserved:
		return "AwaitingAdmission"
	default:
		return "Pending"
	}
}
