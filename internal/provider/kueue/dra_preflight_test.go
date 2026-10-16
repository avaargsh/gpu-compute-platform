package kueue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func testDRADeletionProjection() baseprovider.WorkloadProjection {
	return baseprovider.WorkloadProjection{
		WorkloadID: "train-1", ClusterID: "cluster-a", Namespace: "project-1", Generation: 7,
		AcceleratorBinding: domain.AcceleratorBinding{AllocationMode: domain.AcceleratorAllocationDRA},
	}
}

func assertNoKubernetesDeletes(t *testing.T, client *kubefake.Clientset) {
	t.Helper()
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" || action.GetVerb() == "patch" || action.GetVerb() == "update" {
			t.Fatalf("unexpected mutation on failed read-only preflight: %#v", action)
		}
	}
}

func TestInspectDRAClaimReadOnlyDecisions(t *testing.T) {
	cases := []struct {
		name   string
		claim  *resourcev1.ResourceClaim
		state  string
		reason string
	}{
		{name: "absent", state: ClaimDeletionAbsent},
		{name: "matching", claim: ownedDeletionClaim(), state: ClaimDeletionReady},
		{name: "legacy-unlabeled", claim: func() *resourcev1.ResourceClaim { c := ownedDeletionClaim(); c.Labels = nil; return c }(), state: ClaimDeletionManualReview, reason: "MissingWorkloadOwner"},
		{name: "owner-mismatch", claim: func() *resourcev1.ResourceClaim {
			c := ownedDeletionClaim()
			c.Labels["ai.compute/workload"] = "job-other"
			return c
		}(), state: ClaimDeletionManualReview, reason: "WorkloadOwnerMismatch"},
		{name: "generation-mismatch", claim: func() *resourcev1.ResourceClaim {
			c := ownedDeletionClaim()
			c.Annotations[generationAnnotation] = "8"
			return c
		}(), state: ClaimDeletionManualReview, reason: "GenerationMismatch"},
		{name: "missing-uid", claim: func() *resourcev1.ResourceClaim { c := ownedDeletionClaim(); c.UID = ""; return c }(), state: ClaimDeletionManualReview, reason: "MissingKubernetesIdentity"},
		{name: "missing-version", claim: func() *resourcev1.ResourceClaim { c := ownedDeletionClaim(); c.ResourceVersion = ""; return c }(), state: ClaimDeletionManualReview, reason: "MissingKubernetesIdentity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := kubefake.NewSimpleClientset()
			if tc.claim != nil {
				if err := client.Tracker().Create(resourcev1.SchemeGroupVersion.WithResource("resourceclaims"), tc.claim, tc.claim.Namespace); err != nil {
					t.Fatal(err)
				}
			}
			got, err := NewKubeClient(client, nil).InspectResourceClaim(
				context.Background(), "project-1", "accelerator-train-1", "job-train-1", 7,
			)
			if err != nil || got.State != tc.state || got.Reason != tc.reason {
				t.Fatalf("preflight=%#v err=%v want state=%s reason=%s", got, err, tc.state, tc.reason)
			}
			assertNoKubernetesDeletes(t, client)
		})
	}
}

func TestDRADeletePreflightBlocksLegacyClaimBeforeRemovingJob(t *testing.T) {
	claim := ownedDeletionClaim()
	claim.Labels = nil // Created by the older projector; no ownership label.
	client := kubefake.NewSimpleClientset(ownedDeletionJob(), claim)
	got, err := NewProvider(NewKubeClient(client, nil)).DeleteWorkload(context.Background(), testDRADeletionProjection())
	if err == nil || !strings.Contains(err.Error(), "MissingWorkloadOwner") || got.Gone {
		t.Fatalf("legacy Claim must block Job deletion and require review: got=%#v err=%v", got, err)
	}
	assertNoKubernetesDeletes(t, client)
	if _, err := client.BatchV1().Jobs("project-1").Get(context.Background(), "job-train-1", metav1.GetOptions{}); err != nil {
		t.Fatalf("legacy Claim preflight unexpectedly removed Job: %v", err)
	}
	if _, err := client.ResourceV1().ResourceClaims("project-1").Get(context.Background(), "accelerator-train-1", metav1.GetOptions{}); err != nil {
		t.Fatalf("legacy Claim must be preserved: %v", err)
	}
}

func TestDRADeletePreflightCannotAuthorizeWrongGenerationOrOwner(t *testing.T) {
	tests := []struct {
		name   string
		change func(*resourcev1.ResourceClaim)
	}{
		{"different-generation", func(c *resourcev1.ResourceClaim) { c.Annotations[generationAnnotation] = "8" }},
		{"different-workload", func(c *resourcev1.ResourceClaim) { c.Labels["ai.compute/workload"] = "job-other" }},
		{"identity-missing", func(c *resourcev1.ResourceClaim) { c.UID = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claim := ownedDeletionClaim()
			tc.change(claim)
			client := kubefake.NewSimpleClientset(ownedDeletionJob(), claim)
			_, err := NewProvider(NewKubeClient(client, nil)).DeleteWorkload(context.Background(), testDRADeletionProjection())
			if err == nil || !strings.Contains(err.Error(), "operator review") {
				t.Fatalf("must fail closed before removing job: %v", err)
			}
			assertNoKubernetesDeletes(t, client)
		})
	}
}

func TestDRADeletePreflightForbiddenReadCannotBeTreatedAsAbsence(t *testing.T) {
	client := kubefake.NewSimpleClientset(ownedDeletionJob(), ownedDeletionClaim())
	client.PrependReactor("get", "resourceclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(resourcev1.Resource("resourceclaims"), "accelerator-train-1", errors.New("RBAC denied"))
	})
	_, err := NewProvider(NewKubeClient(client, nil)).DeleteWorkload(context.Background(), testDRADeletionProjection())
	if err == nil || !apierrors.IsForbidden(err) {
		t.Fatalf("forbidden read must surface as error, got %v", err)
	}
	assertNoKubernetesDeletes(t, client)
}

func TestDRADeletePreflightFollowedByGuardedResourceCleanup(t *testing.T) {
	client := kubefake.NewSimpleClientset(ownedDeletionJob(), ownedDeletionClaim())
	got, err := NewProvider(NewKubeClient(client, nil)).DeleteWorkload(context.Background(), testDRADeletionProjection())
	if err != nil || !got.Gone || len(got.EvidenceRefs) != 2 {
		t.Fatalf("expected two independently absent provider resources: got=%#v err=%v", got, err)
	}
	actions := client.Actions()
	var order []string
	for _, action := range actions {
		if action.GetVerb() == "delete" {
			order = append(order, action.GetResource().Resource)
		}
	}
	if len(order) != 2 || order[0] != "jobs" || order[1] != "resourceclaims" {
		t.Fatalf("expected Job-before-Claim guarded DELETE: %#v", order)
	}
}

func TestDRADeletePreflightAbsentClaimStillRemovesJob(t *testing.T) {
	client := kubefake.NewSimpleClientset(ownedDeletionJob())
	got, err := NewProvider(NewKubeClient(client, nil)).DeleteWorkload(context.Background(), testDRADeletionProjection())
	if err != nil || !got.Gone {
		t.Fatalf("no Claim must not block independent Job removal: got=%#v err=%v", got, err)
	}
}

func TestDRAClaimChangedAfterPreflightIsStillGuardedByDeletion(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset(ownedDeletionJob(), ownedDeletionClaim())
	reads := 0
	client.PrependReactor("get", "resourceclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
		reads++
		if reads != 2 {
			return false, nil, nil
		}
		replacement := ownedDeletionClaim()
		replacement.Annotations[generationAnnotation] = "8"
		if err := client.Tracker().Update(resourcev1.SchemeGroupVersion.WithResource("resourceclaims"), replacement, "project-1"); err != nil {
			t.Fatal(err)
		}
		return true, replacement, nil
	})
	got, err := NewProvider(NewKubeClient(client, nil)).DeleteWorkload(ctx, testDRADeletionProjection())
	if err == nil || !strings.Contains(err.Error(), "generation drift") || got.Gone {
		t.Fatalf("post-preflight generation drift must not claim Gone: got=%#v err=%v", got, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" && action.GetResource().Resource == "resourceclaims" {
			t.Fatal("stale preflight must not authorize Claim DELETE")
		}
	}
	newClaim, err := client.ResourceV1().ResourceClaims("project-1").Get(ctx, "accelerator-train-1", metav1.GetOptions{})
	if err != nil || newClaim.Annotations[generationAnnotation] != "8" {
		t.Fatalf("replacement Claim must survive: %#v err=%v", newClaim, err)
	}
}
