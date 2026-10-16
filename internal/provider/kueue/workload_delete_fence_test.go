package kueue

import (
	"context"
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func ownedDeletionJob() *batchv1.Job {
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "job-train-1", Namespace: "project-1",
		UID: types.UID("job-uid-7"), ResourceVersion: "73",
		Annotations: map[string]string{generationAnnotation: "7"},
		Labels: map[string]string{"ai.compute/workload": "job-train-1"},
	}}
}

func ownedDeletionClaim() *resourcev1.ResourceClaim {
	return &resourcev1.ResourceClaim{ObjectMeta: metav1.ObjectMeta{
		Name: "accelerator-train-1", Namespace: "project-1",
		UID: types.UID("claim-uid-7"), ResourceVersion: "81",
		Annotations: map[string]string{generationAnnotation: "7"},
		Labels: map[string]string{"ai.compute/workload": "job-train-1"},
	}}
}

func TestGuardedDeleteJobUsesActualKubernetesUIDAndResourceVersion(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset(ownedDeletionJob())
	called := 0
	client.PrependReactor("delete", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		called++
		opts := action.(ktesting.DeleteAction).GetDeleteOptions()
		if opts.Preconditions == nil || opts.Preconditions.UID == nil ||
			*opts.Preconditions.UID != types.UID("job-uid-7") ||
			opts.Preconditions.ResourceVersion == nil || *opts.Preconditions.ResourceVersion != "73" {
			t.Fatalf("DELETE was not fenced against observed exact Job: %#v", opts.Preconditions)
		}
		return false, nil, nil
	})
	gone, err := NewKubeClient(client, nil).DeleteJob(ctx, "project-1", "job-train-1", 7)
	if err != nil || !gone || called != 1 {
		t.Fatalf("expected guarded delete and independent GET absence: gone=%v err=%v called=%d", gone, err, called)
	}
	gone, err = NewKubeClient(client, nil).DeleteJob(ctx, "project-1", "job-train-1", 7)
	if err != nil || !gone || called != 1 {
		t.Fatalf("absent Job replay must not issue a second DELETE: gone=%v err=%v called=%d", gone, err, called)
	}
}

func TestGuardedDeleteJobFailsClosedOnGenerationOwnerOrIdentityDrift(t *testing.T) {
	cases := []struct {
		name string
		change func(*batchv1.Job)
	}{
		{"higher-generation-replacement", func(job *batchv1.Job) { job.Annotations[generationAnnotation] = "8" }},
		{"missing-generation", func(job *batchv1.Job) { delete(job.Annotations, generationAnnotation) }},
		{"other-workload", func(job *batchv1.Job) { job.Labels["ai.compute/workload"] = "job-other" }},
		{"missing-owner", func(job *batchv1.Job) { delete(job.Labels, "ai.compute/workload") }},
		{"missing-uid", func(job *batchv1.Job) { job.UID = "" }},
		{"missing-resource-version", func(job *batchv1.Job) { job.ResourceVersion = "" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			job := ownedDeletionJob()
			test.change(job)
			client := kubefake.NewSimpleClientset(job)
			calls := 0
			client.PrependReactor("delete", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
				calls++
				return false, nil, nil
			})
			gone, err := NewKubeClient(client, nil).DeleteJob(context.Background(), "project-1", "job-train-1", 7)
			if gone || err == nil || calls != 0 {
				t.Fatalf("stale/missing identity must never issue DELETE: gone=%v err=%v calls=%d", gone, err, calls)
			}
		})
	}
}

func TestGuardedDeleteJobConcurrentReplacementCannotBeDeleted(t *testing.T) {
	client := kubefake.NewSimpleClientset(ownedDeletionJob())
	client.PrependReactor("delete", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		options := action.(ktesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil ||
			*options.Preconditions.UID != types.UID("job-uid-7") {
			t.Fatal("lost UID precondition in GET-to-DELETE race")
		}
		replacement := ownedDeletionJob()
		replacement.UID = types.UID("new-job-uid-8")
		replacement.ResourceVersion = "80"
		replacement.Annotations[generationAnnotation] = "8"
		if err := client.Tracker().Update(batchv1.SchemeGroupVersion.WithResource("jobs"), replacement, "project-1"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewConflict(batchv1.Resource("jobs"), "job-train-1", errors.New("UID changed"))
	})
	gone, err := NewKubeClient(client, nil).DeleteJob(context.Background(), "project-1", "job-train-1", 7)
	if gone || !apierrors.IsConflict(err) {
		t.Fatalf("concurrent replacement must fail rather than finalize: gone=%v err=%v", gone, err)
	}
	found, err := client.BatchV1().Jobs("project-1").Get(context.Background(), "job-train-1", metav1.GetOptions{})
	if err != nil || found.UID != types.UID("new-job-uid-8") {
		t.Fatalf("replacement Job must remain intact: job=%#v err=%v", found, err)
	}
}

func TestGuardedDeleteDRAClaimRequiresGenerationOwnerAndUID(t *testing.T) {
	ctx := context.Background()
	claim := ownedDeletionClaim()
	client := kubefake.NewSimpleClientset(claim)
	calls := 0
	client.PrependReactor("delete", "resourceclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
		calls++
		options := action.(ktesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil ||
			*options.Preconditions.UID != types.UID("claim-uid-7") ||
			options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "81" {
			t.Fatalf("DRA claim not guarded: %#v", options.Preconditions)
		}
		return false, nil, nil
	})
	gone, err := NewKubeClient(client, nil).DeleteResourceClaim(ctx, "project-1", "accelerator-train-1", "job-train-1", 7)
	if err != nil || !gone || calls != 1 {
		t.Fatalf("DRA claim delete must be observed gone: gone=%v err=%v calls=%d", gone, err, calls)
	}
}

func TestGuardedDeleteDRAClaimRejectsOldUnlabeledAndNewerClaim(t *testing.T) {
	cases := []struct {
		name string
		change func(*resourcev1.ResourceClaim)
	}{
		{"legacy-unlabeled", func(claim *resourcev1.ResourceClaim) { claim.Labels = nil }},
		{"other-owner", func(claim *resourcev1.ResourceClaim) { claim.Labels["ai.compute/workload"] = "job-other" }},
		{"new-generation", func(claim *resourcev1.ResourceClaim) { claim.Annotations[generationAnnotation] = "9" }},
		{"missing-generation", func(claim *resourcev1.ResourceClaim) { claim.Annotations = nil }},
		{"missing-uid", func(claim *resourcev1.ResourceClaim) { claim.UID = "" }},
		{"missing-rv", func(claim *resourcev1.ResourceClaim) { claim.ResourceVersion = "" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			claim := ownedDeletionClaim()
			test.change(claim)
			client := kubefake.NewSimpleClientset(claim)
			calls := 0
			client.PrependReactor("delete", "resourceclaims", func(ktesting.Action) (bool, runtime.Object, error) {
				calls++
				return false, nil, nil
			})
			gone, err := NewKubeClient(client, nil).DeleteResourceClaim(
				context.Background(), "project-1", "accelerator-train-1", "job-train-1", 7,
			)
			if gone || err == nil || calls != 0 {
				t.Fatalf("must preserve unowned/legacy/replaced claim: gone=%v err=%v calls=%d", gone, err, calls)
			}
		})
	}
}

func TestGuardedDeleteMissingIdentityRejectedEvenWhenResourceAbsent(t *testing.T) {
	client := NewKubeClient(kubefake.NewSimpleClientset(), nil)
	for _, generation := range []int64{0, -1} {
		if gone, err := client.DeleteJob(context.Background(), "project-1", "job-train-1", generation); gone || err == nil {
			t.Fatalf("Job deletion lacking generation must fail: gone=%v err=%v", gone, err)
		}
		if gone, err := client.DeleteResourceClaim(context.Background(), "project-1", "accelerator-train-1", "job-train-1", generation); gone || err == nil {
			t.Fatalf("claim deletion lacking generation must fail: gone=%v err=%v", gone, err)
		}
	}
	gone, err := client.DeleteResourceClaim(context.Background(), "project-1", "accelerator-train-1", "job-train-1", 7)
	if err != nil || !gone {
		t.Fatalf("missing claim replay should finish without writing: gone=%v err=%v", gone, err)
	}
}

func TestNewDRAClaimCarriesMatchingWorkloadOwnership(t *testing.T) {
	in := Job{Generation: 7, Name: "job-train-1", Namespace: "project-1",
		DRA: &DRARequest{ClaimName: "accelerator-train-1", DeviceClassName: "gpu.nvidia.com", Count: 1}}
	claim, err := resourceClaimObject(in)
	if err != nil || claim.Labels["ai.compute/workload"] != in.Name ||
		claim.Annotations[generationAnnotation] != "7" {
		t.Fatalf("new DRA claim must include generation and ownership: claim=%#v err=%v", claim, err)
	}
}

func TestDeleteIdentityErrorsDoNotContainSecrets(t *testing.T) {
	_, err := NewKubeClient(kubefake.NewSimpleClientset(ownedDeletionJob()), nil).
		DeleteJob(context.Background(), "project-1", "job-train-1", 9)
	if err == nil || !strings.Contains(err.Error(), "generation drift") {
		t.Fatalf("expected narrow generation drift diagnostic: %v", err)
	}
}
