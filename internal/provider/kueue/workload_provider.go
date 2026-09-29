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
	if job.DRA != nil {
		if err := p.client.ApplyResourceClaim(ctx, job); err != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("apply resource claim: %w", classifyProviderError(err))
		}
	}
	if err := p.client.ApplyJob(ctx, job); err != nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("apply job: %w", classifyProviderError(err))
	}

	state, err := p.client.ObserveJob(ctx, job.Namespace, job.Name)
	if err != nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("observe job: %w", classifyProviderError(err))
	}

	now := time.Now().UTC()
	conditions := []domain.Condition{
		{
			Type:               "Ready",
			Status:             boolStatus((state.PodsReady || state.Succeeded) && !state.Failed),
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
			Reason:             podsReadyReason(state),
			LastTransitionTime: now,
		},
	}
	if state.Succeeded {
		conditions = append(conditions, domain.Condition{
			Type:               "Succeeded",
			Status:             "True",
			Reason:             "JobSucceeded",
			LastTransitionTime: now,
		})
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
	if job.DRA != nil {
		evidenceRefs = append(evidenceRefs, fmt.Sprintf("k8s://%s/namespaces/%s/resourceclaims/%s", projection.ClusterID, job.Namespace, job.DRA.ClaimName))
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

func podsReadyReason(state JobObservation) string {
	switch {
	case state.PodsReady:
		return "PodsReady"
	case state.Failed:
		return "JobFailed"
	case state.Succeeded:
		return "JobCompleted"
	default:
		return "AwaitingPods"
	}
}

func readyReason(state JobObservation) string {
	switch {
	case state.Failed:
		return "JobFailed"
	case state.Succeeded:
		return "JobSucceeded"
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

func (p *Provider) DeleteWorkload(ctx context.Context, projection baseprovider.WorkloadProjection) (baseprovider.DeletionObservation, error) {
	if p.client == nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("kueue client is required")
	}
	if projection.WorkloadID == "" || projection.Namespace == "" {
		return baseprovider.DeletionObservation{}, fmt.Errorf("workload and namespace are required")
	}
	jobName := resourceName("job", string(projection.WorkloadID))
	job, projectErr := ProjectWorkload(projection)
	if projectErr != nil {
		return baseprovider.DeletionObservation{}, projectErr
	}
	gone, err := p.client.DeleteJob(ctx, projection.Namespace, jobName)
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("delete job: %w", classifyProviderError(err))
	}
	if !gone {
		return baseprovider.DeletionObservation{Gone: false, EvidenceRefs: []string{fmt.Sprintf("k8s://%s/namespaces/%s/jobs/%s", projection.ClusterID, projection.Namespace, jobName)}}, nil
	}
	if job.DRA != nil {
		claimGone, err := p.client.DeleteResourceClaim(ctx, projection.Namespace, job.DRA.ClaimName)
		if err != nil {
			return baseprovider.DeletionObservation{}, fmt.Errorf("delete resource claim: %w", classifyProviderError(err))
		}
		gone = claimGone
	}
	return baseprovider.DeletionObservation{
		Gone: gone,
		EvidenceRefs: []string{
			fmt.Sprintf("k8s://%s/namespaces/%s/jobs/%s", projection.ClusterID, projection.Namespace, jobName),
		},
	}, nil
}
