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

	condition := domain.Condition{
		Type:               "Ready",
		Status:             "False",
		Reason:             "Pending",
		Message:            state.Message,
		LastTransitionTime: time.Now().UTC(),
	}
	switch {
	case state.Failed:
		condition.Type = "Failed"
		condition.Reason = "JobFailed"
	case state.PodsReady:
		condition.Status = "True"
		condition.Reason = "PodsReady"
	case state.Admitted:
		condition.Type = "Admitted"
		condition.Status = "True"
		condition.Reason = "KueueAdmitted"
	}

	return baseprovider.WorkloadObservation{
		ObservedGeneration: projection.Generation,
		Phase:              state.Phase,
		Conditions:         []domain.Condition{condition},
		EvidenceRefs: []string{
			fmt.Sprintf("k8s://%s/namespaces/%s/jobs/%s", projection.ClusterID, job.Namespace, job.Name),
		},
	}, nil
}
