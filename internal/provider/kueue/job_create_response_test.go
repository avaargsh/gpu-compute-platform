package kueue

import (
	"context"
	"errors"
	"strings"
	"testing"

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
