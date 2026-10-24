package kueue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func responseJob() Job {
	return Job{
		Generation: 7, Name: "job-train", Namespace: "research",
		Image:     "example/train@sha256:" + strings.Repeat("a", 64),
		Resources: map[string]int64{"vendor.example/gpu": 1},
	}
}

func TestJobCreateResponseOnlyFromActualAPIWrite(t *testing.T) {
	client := kubefake.NewSimpleClientset()
	creates := 0
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		creates++
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID = types.UID("apiserver-created-job-uid")
		job.ResourceVersion = "101"
		return true, job, nil
	})
	result, err := NewKubeClient(client, nil).ApplyJobWithCreateResponse(context.Background(), responseJob())
	if err != nil || creates != 1 || result.Disposition != JobCreateAcknowledged ||
		result.UID != types.UID("apiserver-created-job-uid") || result.ResourceVersion != "101" {
		t.Fatalf("expected only actual CREATE return: %#v err=%v calls=%d", result, err, creates)
	}
}

func TestJobExistingSameGenerationNeverYieldsCreateIdentity(t *testing.T) {
	job, err := jobObject(responseJob())
	if err != nil {
		t.Fatal(err)
	}
	job.UID = "uid-from-existing-object"
	job.ResourceVersion = "300"
	client := kubefake.NewSimpleClientset(job)
	result, err := NewKubeClient(client, nil).ApplyJobWithCreateResponse(context.Background(), responseJob())
	if err != nil || result.Disposition != JobExistingUnbound || result.UID != "" || result.ResourceVersion != "" {
		t.Fatalf("existing Job must never masquerade as a CREATE response: %#v err=%v", result, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" {
			t.Fatal("existing Job should not trigger CREATE")
		}
	}
}

func TestJobCreateRaceAdoptionNeverYieldsCreateIdentity(t *testing.T) {
	client := kubefake.NewSimpleClientset()
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID = "race-winner-uid"
		job.ResourceVersion = "400"
		if err := client.Tracker().Create(batchv1.SchemeGroupVersion.WithResource("jobs"), job, action.GetNamespace()); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewAlreadyExists(batchv1.Resource("jobs"), job.Name)
	})
	result, err := NewKubeClient(client, nil).ApplyJobWithCreateResponse(context.Background(), responseJob())
	if err != nil || result.Disposition != JobRaceAdoptedUnbound || result.UID != "" || result.ResourceVersion != "" {
		t.Fatalf("GET after AlreadyExists cannot mint CREATE response: %#v err=%v", result, err)
	}
}

func TestJobLostCreateACKCannotMintUIDFromRecoveryGET(t *testing.T) {
	client := kubefake.NewSimpleClientset()
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID = "persisted-but-no-ack"
		job.ResourceVersion = "404"
		if err := client.Tracker().Create(batchv1.SchemeGroupVersion.WithResource("jobs"), job, action.GetNamespace()); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewTimeoutError("lost response", 1)
	})
	kube := NewKubeClient(client, nil)
	if got, err := kube.ApplyJobWithCreateResponse(context.Background(), responseJob()); !apierrors.IsTimeout(err) || got.UID != "" {
		t.Fatalf("lost ACK must be UNKNOWN: %#v %v", got, err)
	}
	result, err := kube.ApplyJobWithCreateResponse(context.Background(), responseJob())
	if err != nil || result.Disposition != JobExistingUnbound || result.UID != "" {
		t.Fatalf("recovery may converge but never invent CREATE provenance: %#v %v", result, err)
	}
}

func TestMissingCreateUIDAndVersionFailClosedForResponseConsumer(t *testing.T) {
	client := kubefake.NewSimpleClientset()
	_, err := NewKubeClient(client, nil).ApplyJobWithCreateResponse(context.Background(), responseJob())
	if err == nil || !strings.Contains(err.Error(), "missing API-server UID") {
		t.Fatalf("unbound CREATE accepted: %v", err)
	}
	// Legacy reconciliation intentionally retains its pre-existing contract,
	// without ever emitting a proof or forcing an operation UID binding.
	legacyClient := kubefake.NewSimpleClientset()
	if err := NewKubeClient(legacyClient, nil).ApplyJob(context.Background(), responseJob()); err != nil {
		t.Fatalf("legacy at-least-once semantics were broken: %v", err)
	}
}

func TestJobCreateResponseFailClosedOnForbiddenAndGenerationDrift(t *testing.T) {
	forbiddenClient := kubefake.NewSimpleClientset()
	forbiddenClient.PrependReactor("get", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(batchv1.Resource("jobs"), action.(ktesting.GetAction).GetName(), errors.New("forbidden"))
	})
	if r, err := NewKubeClient(forbiddenClient, nil).ApplyJobWithCreateResponse(context.Background(), responseJob()); !apierrors.IsForbidden(err) || r.UID != "" {
		t.Fatalf("forbidden GET cannot yield CREATE response: %#v err=%v", r, err)
	}
	job, err := jobObject(responseJob())
	if err != nil {
		t.Fatal(err)
	}
	job.UID = types.UID("older-job")
	job.ResourceVersion = "55"
	job.Annotations[generationAnnotation] = "6"
	driftClient := kubefake.NewSimpleClientset(job)
	if r, err := NewKubeClient(driftClient, nil).ApplyJobWithCreateResponse(context.Background(), responseJob()); err == nil || r.UID != "" {
		t.Fatalf("generation drift must fail closed: %#v err=%v", r, err)
	}
}

func TestGuardedKubernetesCREATEPreparesOnlyWhenActuallyNeeded(t *testing.T) {
	ctx := context.Background()
	owned, err := jobObject(responseJob())
	if err != nil {
		t.Fatal(err)
	}
	owned.UID = types.UID("already-there")
	owned.ResourceVersion = "22"
	existing := kubefake.NewSimpleClientset(owned)
	prepared := 0
	got, err := NewKubeClient(existing, nil).ApplyJobWithGuardedCreate(ctx, responseJob(),
		func(context.Context) error { prepared++; return nil })
	if err != nil || got.Disposition != JobExistingUnbound || prepared != 0 {
		t.Fatalf("same-name GET must not prepare a new CREATE: %#v %v prepared %d", got, err, prepared)
	}
	absent := kubefake.NewSimpleClientset()
	got, err = NewKubeClient(absent, nil).ApplyJobWithGuardedCreate(ctx, responseJob(),
		func(context.Context) error { prepared++; return verifiedops.ErrCreateAttemptUnresolved })
	if !errors.Is(err, verifiedops.ErrCreateAttemptUnresolved) || got.UID != "" || prepared != 1 {
		t.Fatalf("unresolved durable attempt allowed CREATE: %#v err %v prepared %d", got, err, prepared)
	}
	for _, action := range absent.Actions() {
		if action.GetVerb() == "create" {
			t.Fatalf("Kubernetes CREATE occurred despite failed prepare: %#v", absent.Actions())
		}
	}
	// The original method remains a separate unverified provider API.
	if _, err := NewKubeClient(absent, nil).ApplyJobWithGuardedCreate(ctx, responseJob(), nil); err == nil {
		t.Fatal("nil pre-CREATE guard accepted")
	}
}

func TestGuardedKubernetesCREATEMarksLostACKAsUnbound(t *testing.T) {
	client := kubefake.NewSimpleClientset()
	prepared := 0
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		job.UID = "ack-was-lost"
		job.ResourceVersion = "404"
		if err := client.Tracker().Create(batchv1.SchemeGroupVersion.WithResource("jobs"), job, action.GetNamespace()); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewTimeoutError("ack lost after real API write", 1)
	})
	kube := NewKubeClient(client, nil)
	first, err := kube.ApplyJobWithGuardedCreate(context.Background(), responseJob(), func(context.Context) error { prepared++; return nil })
	if !apierrors.IsTimeout(err) || first.UID != "" || prepared != 1 {
		t.Fatalf("lost ACK: %#v %v prepared=%d", first, err, prepared)
	}
	next, err := kube.ApplyJobWithGuardedCreate(context.Background(), responseJob(), func(context.Context) error { prepared++; return nil })
	if err != nil || next.Disposition != JobExistingUnbound || next.UID != "" || prepared != 1 {
		t.Fatalf("later GET invented CREATE or prepared again: %#v %v prepared=%d", next, err, prepared)
	}
}

func TestACKBoundInspectionRequiresExactLiveUIDAndGeneration(t *testing.T) {
	job, err := jobObject(responseJob())
	if err != nil {
		t.Fatal(err)
	}
	job.UID = types.UID("bound-job-uid")
	job.ResourceVersion = "42"
	core := kubefake.NewSimpleClientset(job)
	kube := NewKubeClient(core, nil)
	if err := kube.InspectBoundJob(context.Background(), responseJob(), "bound-job-uid"); err != nil {
		t.Fatalf("exact ACK-bound Job should be inspectable: %v", err)
	}
	observed, err := kube.ObserveJob(context.Background(), job.Namespace, job.Name)
	if err != nil || observed.JobUID != "bound-job-uid" {
		t.Fatalf("observation did not preserve same-read Job UID: %#v err=%v", observed, err)
	}
	if err := kube.InspectBoundJob(context.Background(), responseJob(), "replacement-uid"); !errors.Is(err, verifiedops.ErrCreateUIDUnproven) {
		t.Fatalf("same-name replacement UID was accepted: %v", err)
	}
	staleJob, err := jobObject(responseJob())
	if err != nil {
		t.Fatal(err)
	}
	staleJob.UID = types.UID("old-generation-job")
	staleJob.ResourceVersion = "43"
	staleJob.Annotations[generationAnnotation] = "6"
	stale := NewKubeClient(kubefake.NewSimpleClientset(staleJob), nil)
	if err := stale.InspectBoundJob(context.Background(), responseJob(), "old-generation-job"); !errors.Is(err, verifiedops.ErrCreateUIDUnproven) {
		t.Fatalf("stale generation was accepted as ACK-bound: %v", err)
	}
	absentCore := kubefake.NewSimpleClientset()
	absent := NewKubeClient(absentCore, nil)
	if err := absent.InspectBoundJob(context.Background(), responseJob(), "bound-job-uid"); !errors.Is(err, verifiedops.ErrCreateUIDUnproven) {
		t.Fatalf("absent ACK-bound Job must not trigger replacement: %v", err)
	}
	for _, action := range absentCore.Actions() {
		if action.GetVerb() == "create" {
			t.Fatalf("ACK-bound recovery issued CREATE: %#v", absentCore.Actions())
		}
	}
}


// A DRA claim is a provider mutation too. The durable PRE-CREATE guard must win
// before either claim creation or Job creation, while an admitted attempt still
// creates the claim before the Job that references it.
func TestGuardedKubernetesCREATEPreparesBeforeDRAClaim(t *testing.T) {
	ctx := context.Background()
	job := responseJob()
	job.DRA = &DRARequest{
		ClaimName:       "accelerator-train",
		DeviceClassName: "gpu.nvidia.com",
		Count:           1,
	}
	client := kubefake.NewSimpleClientset()
	var events []string
	client.PrependReactor("create", "resourceclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		events = append(events, "claim")
		return false, nil, nil
	})
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		events = append(events, "job")
		created := action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		created.UID = types.UID("apiserver-created-dra-job")
		created.ResourceVersion = "501"
		return true, created, nil
	})

	result, err := NewKubeClient(client, nil).ApplyJobWithGuardedCreate(ctx, job, func(context.Context) error {
		events = append(events, "prepared")
		return nil
	})
	if err != nil || result.Disposition != JobCreateAcknowledged ||
		result.UID != types.UID("apiserver-created-dra-job") || result.ResourceVersion != "501" {
		t.Fatalf("guarded DRA Job CREATE failed: %#v err=%v", result, err)
	}
	want := []string{"prepared", "claim", "job"}
	if len(events) != len(want) {
		t.Fatalf("unexpected side-effect sequence: got %#v want %#v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event[%d]=%q, want %q; sequence=%#v", i, events[i], want[i], events)
		}
	}

	// A refused PRE-CREATE must leave both external resources untouched.
	refusedClient := kubefake.NewSimpleClientset()
	refused := 0
	_, err = NewKubeClient(refusedClient, nil).ApplyJobWithGuardedCreate(ctx, job, func(context.Context) error {
		refused++
		return verifiedops.ErrCreateAttemptUnresolved
	})
	if !errors.Is(err, verifiedops.ErrCreateAttemptUnresolved) || refused != 1 {
		t.Fatalf("unresolved durable attempt was not refused exactly once: err=%v calls=%d", err, refused)
	}
	for _, action := range refusedClient.Actions() {
		if action.GetVerb() == "create" &&
			(action.GetResource().Resource == "jobs" || action.GetResource().Resource == "resourceclaims") {
			t.Fatalf("refused durable attempt mutated Kubernetes: %#v", refusedClient.Actions())
		}
	}
}
