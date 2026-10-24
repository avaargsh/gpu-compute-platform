package kueue

import (
	"context"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

func (p *Provider) ReconcileWorkload(ctx context.Context, projection baseprovider.WorkloadProjection) (baseprovider.WorkloadObservation, error) {
	if p.client == nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("kueue client is required")
	}

	job, err := ProjectWorkload(projection)
	if err != nil {
		return baseprovider.WorkloadObservation{}, err
	}
	if projection.ExpectedCreateUID != "" && projection.OperationID == "" {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("expected CREATE UID requires an audited operation")
	}
	// A bound operation is a read-only recovery path. Validate its mode before
	// any provider mutation, including DRA ResourceClaim creation.
	if projection.OperationID != "" {
		if projection.ExpectedCreateUID == "" {
			if projection.OnBeforeCreate == nil || projection.OnCreateACK == nil {
				return baseprovider.WorkloadObservation{}, fmt.Errorf("claimed CREATE requires durable attempt resolution, PREPARED and ACK publisher")
			}
		} else if projection.OnBeforeCreate != nil || projection.OnCreateACK != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("ACK-bound recovery must not carry CREATE callbacks")
		}
	}

	// Legacy/unclaimed reconciliation retains its existing DRA flow. For an
	// audited create, the guarded KubeClient performs this write only after the
	// durable PRE-CREATE callback succeeds.
	if projection.OperationID == "" && projection.ExpectedCreateUID == "" && job.DRA != nil {
		if err := p.client.ApplyResourceClaim(ctx, job); err != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("apply resource claim: %w", classifyProviderError(err))
		}
	}

	if projection.OperationID != "" && projection.ExpectedCreateUID != "" {
		// Never CREATE or repair side effects for an ACK-bound operation. The
		// original Job must still be present under the same API-server UID.
		inspector, ok := p.client.(interface {
			InspectBoundJob(context.Context, Job, string) error
		})
		if !ok {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("read-only bound Job inspection unavailable")
		}
		if err := inspector.InspectBoundJob(ctx, job, projection.ExpectedCreateUID); err != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("inspect ACK-bound Job: %w", classifyProviderError(err))
		}
	} else if projection.OperationID != "" {
		// A provider GET or AlreadyExists race is convergence, not proof that
		// this audited operation received the CREATE ACK. Only the exact
		// API-server CREATE response may publish an ACK-bound candidate.
		capture, ok := p.client.(interface {
			ApplyJobWithGuardedCreate(context.Context, Job, func(context.Context) error) (JobCreateResponse, error)
		})
		if !ok {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("Kueue Job CREATE response capture unavailable")
		}
		result, err := capture.ApplyJobWithGuardedCreate(ctx, job, func(callCtx context.Context) error {
			return projection.OnBeforeCreate(callCtx, baseprovider.JobCreateIntent{Namespace: job.Namespace, Name: job.Name})
		})
		if err != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("apply Job with CREATE capture: %w", classifyProviderError(err))
		}
		if result.Disposition != JobCreateAcknowledged {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("%w: provider returned %s without a durable CREATE ACK", verifiedops.ErrCreateUIDUnproven, result.Disposition)
		}
		if result.UID == "" || result.ResourceVersion == "" {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("%w: CREATE response omitted API-server UID/resourceVersion", verifiedops.ErrCreateUIDUnproven)
		}
		if err := projection.OnCreateACK(ctx, baseprovider.JobCreateACK{
			Namespace: job.Namespace, Name: job.Name,
			UID: string(result.UID), ResourceVersion: result.ResourceVersion,
		}); err != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("publish Job CREATE candidate: %w", baseprovider.MarkRetryable(err))
		}
	} else {
		if err := p.client.ApplyJob(ctx, job); err != nil {
			return baseprovider.WorkloadObservation{}, fmt.Errorf("apply job: %w", classifyProviderError(err))
		}
	}
	state, err := p.client.ObserveJob(ctx, job.Namespace, job.Name)
	if err != nil {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("observe job: %w", classifyProviderError(err))
	}
	if projection.ExpectedCreateUID != "" && state.JobUID != projection.ExpectedCreateUID {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("%w: observed Job UID differs from persisted CREATE ACK", verifiedops.ErrCreateUIDUnproven)
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
	if projection.WorkloadID == "" || projection.Namespace == "" || projection.Generation < 1 {
		return baseprovider.DeletionObservation{}, fmt.Errorf("workload, namespace and positive generation are required")
	}

	jobName := resourceName("job", string(projection.WorkloadID))
	evidenceRefs := []string{
		fmt.Sprintf("k8s://%s/namespaces/%s/jobs/%s", projection.ClusterID, projection.Namespace, jobName),
	}

	// Inspect DRA ownership *before* deleting its Job: older claims lack
	// the new ownership label, and deleting the Job first would leave an
	// orphan with a blocked Finalizer. This read is not an authorization
	// token; DeleteResourceClaim rechecks metadata with UID/RV CAS.
	if projection.AcceleratorBinding.AllocationMode == domain.AcceleratorAllocationDRA {
		claimName := resourceName("accelerator", string(projection.WorkloadID))
		assessment, inspectErr := p.client.InspectResourceClaim(
			ctx, projection.Namespace, claimName, jobName, projection.Generation,
		)
		if inspectErr != nil {
			return baseprovider.DeletionObservation{}, fmt.Errorf("inspect resource claim before deleting job: %w", classifyProviderError(inspectErr))
		}
		switch assessment.State {
		case ClaimDeletionReady, ClaimDeletionAbsent:
			// A 404 means no Claim needs removal; the Job can be cleaned up.
		case ClaimDeletionManualReview:
			return baseprovider.DeletionObservation{}, fmt.Errorf(
				"resource claim %s needs operator review before deleting job: %s", claimName, assessment.Reason,
			)
		default:
			return baseprovider.DeletionObservation{}, fmt.Errorf("resource claim preflight returned unsupported decision")
		}
	}

	gone, err := p.client.DeleteJob(ctx, projection.Namespace, jobName, projection.Generation)
	if err != nil {
		return baseprovider.DeletionObservation{}, fmt.Errorf("delete job: %w", classifyProviderError(err))
	}
	if !gone {
		return baseprovider.DeletionObservation{Gone: false, EvidenceRefs: evidenceRefs}, nil
	}

	if projection.AcceleratorBinding.AllocationMode == domain.AcceleratorAllocationDRA {
		claimName := resourceName("accelerator", string(projection.WorkloadID))
		evidenceRefs = append(evidenceRefs,
			fmt.Sprintf("k8s://%s/namespaces/%s/resourceclaims/%s", projection.ClusterID, projection.Namespace, claimName),
		)
		claimGone, err := p.client.DeleteResourceClaim(ctx, projection.Namespace, claimName, jobName, projection.Generation)
		if err != nil {
			return baseprovider.DeletionObservation{}, fmt.Errorf("delete resource claim: %w", classifyProviderError(err))
		}
		gone = claimGone
	}

	return baseprovider.DeletionObservation{
		Gone:         gone,
		EvidenceRefs: evidenceRefs,
	}, nil
}
