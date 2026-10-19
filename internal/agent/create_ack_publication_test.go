package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

type scriptedACKRuntime struct {
	*fakeRuntime
	ack provider.JobCreateACK
}

func (r *scriptedACKRuntime) ReconcileWorkload(ctx context.Context, p provider.WorkloadProjection) (provider.WorkloadObservation, error) {
	r.workloadCalls++
	r.workload = p
	if p.OnBeforeCreate != nil {
		if err := p.OnBeforeCreate(ctx, provider.JobCreateIntent{Namespace: r.ack.Namespace, Name: r.ack.Name}); err != nil {
			return provider.WorkloadObservation{}, provider.MarkRetryable(err)
		}
	}
	if p.OnCreateACK != nil {
		if err := p.OnCreateACK(ctx, r.ack); err != nil {
			return provider.WorkloadObservation{}, provider.MarkRetryable(err)
		}
	}
	return provider.WorkloadObservation{ObservedGeneration: p.Generation}, nil
}

func (f *fakeControlPlane) PrepareJobCreateAttempt(_ context.Context, in verifiedops.JobCreateAttempt) error {
	f.prepared = append(f.prepared, in)
	return f.prepareErr
}

func (f *fakeControlPlane) PublishJobCreateUID(_ context.Context, in JobCreateUIDCandidate) error {
	f.published = append(f.published, in)
	f.publishedBeforeRelease = f.releaseCalls == 1 // pool lease was released first
	return f.publishErr
}

func createACKRunnerFixture() (*fakeControlPlane, *scriptedACKRuntime, *Runner) {
	control := &fakeControlPlane{desired: []DesiredResource{
		{Kind: "ComputePool", ID: "pool-a", Generation: 1, Spec: map[string]any{
			"projectID": "project-a", "namespace": "project-a",
			"acceleratorBindings": []any{map[string]any{"class": "h100", "resourceName": "nvidia.com/gpu", "flavor": "h100"}},
		}},
		{Kind: "Workload", ID: "train-a", Generation: 1, OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Spec: map[string]any{
			"projectID": "project-a", "poolID": "pool-a", "namespace": "project-a",
			"image": "example/train:1", "accelerator": map[string]any{"class": "h100", "quota": float64(1)},
			"operationId": "user-supplied-not-authoritative",
		}},
	}}
	rt := &scriptedACKRuntime{fakeRuntime: &fakeRuntime{}, ack: provider.JobCreateACK{
		Namespace: "project-a", Name: "job-train-a", UID: "api-created-uid", ResourceVersion: "101",
	}}
	return control, rt, NewRunnerWithLeaseOwner(domain.ID("cluster-a"), control, rt, "agent-lease-1")
}

func TestRunnerPublishesServerDerivedCreateBeforeReportAndRelease(t *testing.T) {
	control, rt, runner := createACKRunnerFixture()
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.prepared) != 1 || control.prepared[0].OperationID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		control.prepared[0].JobName != "job-train-a" || control.prepared[0].LeaseOwner != "agent-lease-1" {
		t.Fatalf("durable pre-CREATE missing or forged: %#v", control.prepared)
	}
	if len(control.published) != 1 {
		t.Fatalf("expected one candidate publication: %#v", control.published)
	}
	got := control.published[0]
	if got.OperationID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got.OperationID == "user-supplied-not-authoritative" ||
		got.ClusterID != "cluster-a" || got.ProjectID != "project-a" || got.WorkloadID != "train-a" ||
		got.Generation != 1 || got.JobUID != "api-created-uid" || got.JobResourceVersion != "101" ||
		got.LeaseOwner != "agent-lease-1" || !control.publishedBeforeRelease {
		t.Fatalf("failed scoped durable candidate handoff %#v before-release=%v", got, control.publishedBeforeRelease)
	}
	if len(control.reported) != 2 || rt.workload.OnCreateACK == nil {
		t.Fatalf("missing report or callback: reports=%d callback-present=%v", len(control.reported), rt.workload.OnCreateACK != nil)
	}
}

func TestRunnerCandidatePublishFailureCannotReportReady(t *testing.T) {
	control, _, runner := createACKRunnerFixture()
	control.publishErr = errors.New("Postgres unavailable")
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.published) != 1 || len(control.reported) != 1 || control.releaseCalls != 2 {
		t.Fatalf("stale persisted report or lease: pubs=%d reports=%d release=%d", len(control.published), len(control.reported), control.releaseCalls)
	}
}

func TestRunnerWithoutOriginalAuditCannotMintOperationID(t *testing.T) {
	control, rt, runner := createACKRunnerFixture()
	control.desired[1].OperationID = ""
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.published) != 0 || rt.workload.OnCreateACK != nil {
		t.Fatalf("legacy Desired unexpectedly generated CREATE proof: %#v", control.published)
	}
}

func TestRunnerPreCREATEFailureNeverPublishesACK(t *testing.T) {
	control, _, runner := createACKRunnerFixture()
	control.prepareErr = verifiedops.ErrCreateAttemptUnresolved
	if err := runner.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.prepared) != 1 || len(control.published) != 0 ||
		len(control.reported) != 1 || control.releaseCalls != 2 {
		t.Fatalf("ambiguous pre-CREATE incorrectly published/report success: prepared=%d published=%d report=%d release=%d",
			len(control.prepared), len(control.published), len(control.reported), control.releaseCalls)
	}
}
