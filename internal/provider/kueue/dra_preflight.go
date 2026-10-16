package kueue

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InspectResourceClaim is a read-only deletion preflight. It deliberately
// does NOT approve migrations or mutate legacy ResourceClaims. In particular,
// a missing ai.compute/workload label is not sufficient evidence of ownership.
func (c *KubeClient) InspectResourceClaim(
	ctx context.Context, namespace, claimName, jobName string, generation int64,
) (ClaimDeletionPreflight, error) {
	if c.core == nil {
		return ClaimDeletionPreflight{}, fmt.Errorf("kubernetes client is required")
	}
	if namespace == "" || claimName == "" || jobName == "" || generation < 1 {
		return ClaimDeletionPreflight{}, fmt.Errorf("claim preflight requires namespace, claim, job and positive generation")
	}
	claim, err := c.core.ResourceV1().ResourceClaims(namespace).Get(ctx, claimName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ClaimDeletionPreflight{State: ClaimDeletionAbsent}, nil
	}
	if err != nil {
		// Forbidden, timeout and other failed observations MUST never be
		// misclassified as absence or a deletion approval.
		return ClaimDeletionPreflight{}, err
	}
	if claim.Annotations[generationAnnotation] != strconv.FormatInt(generation, 10) {
		return ClaimDeletionPreflight{State: ClaimDeletionManualReview, Reason: "GenerationMismatch"}, nil
	}
	owner := claim.Labels["ai.compute/workload"]
	if owner == "" {
		return ClaimDeletionPreflight{State: ClaimDeletionManualReview, Reason: "MissingWorkloadOwner"}, nil
	}
	if owner != jobName {
		return ClaimDeletionPreflight{State: ClaimDeletionManualReview, Reason: "WorkloadOwnerMismatch"}, nil
	}
	if claim.UID == "" || claim.ResourceVersion == "" {
		return ClaimDeletionPreflight{State: ClaimDeletionManualReview, Reason: "MissingKubernetesIdentity"}, nil
	}
	return ClaimDeletionPreflight{State: ClaimDeletionReady}, nil
}
