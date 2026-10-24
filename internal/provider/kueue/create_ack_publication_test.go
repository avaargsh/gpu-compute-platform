package kueue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	"k8s.io/apimachinery/pkg/types"
)

type ackCaptureClient struct {
	*fakeClient
	response JobCreateResponse
	err      error
	captures int
}

func (c *ackCaptureClient) ApplyJobWithGuardedCreate(ctx context.Context, _ Job, before func(context.Context) error) (JobCreateResponse, error) {
	c.captures++
	if c.response.Disposition == JobCreateAcknowledged || c.response.Disposition == JobRaceAdoptedUnbound {
		if err := before(ctx); err != nil {
			return JobCreateResponse{}, err
		}
	}
	return c.response, c.err
}

func createACKProjection() baseprovider.WorkloadProjection {
	return baseprovider.WorkloadProjection{
		Provider: "kueue", ClusterID: "cluster-a", ProjectID: "project-a",
		PoolID: "pool-a", WorkloadID: "train-a", Namespace: "project-a",
		Generation: 1, OperationID: strings.Repeat("a", 32),
		Image:              "example/train@sha256:" + strings.Repeat("b", 64),
		Accelerator:        domain.AcceleratorRequest{Class: "h100", Quota: 1},
		AcceleratorBinding: domain.AcceleratorBinding{Class: "h100", ResourceName: "nvidia.com/gpu", Flavor: "h100"},
	}
}

func TestKueueOnlyPublishesActualAPICreateACK(t *testing.T) {
	for _, tc := range []struct {
		name string
		disposition JobApplyDisposition
		wantCalls int
		wantErr bool
	}{
		{"created", JobCreateAcknowledged, 1, false},
		{"existing", JobExistingUnbound, 0, true},
		{"already-exists-race", JobRaceAdoptedUnbound, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &ackCaptureClient{fakeClient: &fakeClient{}, response: JobCreateResponse{
				Disposition: tc.disposition, UID: types.UID("returned-uid"), ResourceVersion: "17",
			}}
			p := createACKProjection()
			calls := 0
			p.OnBeforeCreate = func(context.Context, baseprovider.JobCreateIntent) error { return nil }
			p.OnCreateACK = func(_ context.Context, ack baseprovider.JobCreateACK) error {
				calls++
				if ack.UID != "returned-uid" || ack.ResourceVersion != "17" || ack.Name != "job-train-a" || ack.Namespace != "project-a" {
					t.Fatalf("unexpected actual ACK: %#v", ack)
				}
				return nil
			}
			_, err := NewProvider(c).ReconcileWorkload(context.Background(), p)
			if tc.wantErr {
				if !errors.Is(err, verifiedops.ErrCreateUIDUnproven) || calls != 0 || c.captures != 1 {
					t.Fatalf("unbound disposition was promoted: capture=%d publish=%d err=%v", c.captures, calls, err)
				}
				return
			}
			if err != nil || c.captures != 1 || calls != tc.wantCalls {
				t.Fatalf("decision %s: capture=%d publish=%d err=%v", tc.disposition, c.captures, calls, err)
			}
		})
	}
}
func TestKueuePublishFailureStopsObservationAndReportsRetryable(t *testing.T) {
	c := &ackCaptureClient{fakeClient: &fakeClient{}, response: JobCreateResponse{Disposition: JobCreateAcknowledged, UID: "uid", ResourceVersion: "9"}}
	p := createACKProjection()
	p.OnBeforeCreate = func(context.Context, baseprovider.JobCreateIntent) error { return nil }
	p.OnCreateACK = func(context.Context, baseprovider.JobCreateACK) error { return errors.New("disconnected") }
	_, err := NewProvider(c).ReconcileWorkload(context.Background(), p)
	if err == nil || !baseprovider.IsRetryable(err) {
		t.Fatalf("persistence failure must retry: %v", err)
	}
	for _, operation := range c.order {
		if strings.HasPrefix(operation, "job:") {
			t.Fatalf("fell back to a second non-proof CREATE: %#v", c.order)
		}
	}
}
func TestKueueCannotCreateWithClaimedOperationOnLegacyAdapter(t *testing.T) {
	p := createACKProjection()
	p.OnBeforeCreate = func(context.Context, baseprovider.JobCreateIntent) error { return nil }
	p.OnCreateACK = func(context.Context, baseprovider.JobCreateACK) error { return nil }
	_, err := NewProvider(&fakeClient{}).ReconcileWorkload(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "capture unavailable") {
		t.Fatalf("missing CREATE seam accepted: %v", err)
	}
}
func TestKueueTimeoutCannotPublishACK(t *testing.T) {
	c := &ackCaptureClient{fakeClient: &fakeClient{}, err: errors.New("lost ACK")}
	p := createACKProjection()
	calls := 0
	p.OnBeforeCreate = func(context.Context, baseprovider.JobCreateIntent) error { return nil }
	p.OnCreateACK = func(context.Context, baseprovider.JobCreateACK) error { calls++; return nil }
	_, err := NewProvider(c).ReconcileWorkload(context.Background(), p)
	if err == nil || calls != 0 {
		t.Fatalf("ambiguous CREATE became candidate proof: %v calls %d", err, calls)
	}
}

type boundInspectTestClient struct {
	*fakeClient
	expectedUID string
	observedUID string
	inspectErr error
	inspectCalls int
}

func (c *boundInspectTestClient) InspectBoundJob(_ context.Context, job Job, expectedUID string) error {
	c.inspectCalls++
	c.order = append(c.order, "inspect-bound:"+job.Name)
	if expectedUID != c.expectedUID { return verifiedops.ErrCreateUIDUnproven }
	return c.inspectErr
}

func (c *boundInspectTestClient) ObserveJob(context.Context, string, string) (JobObservation, error) {
	return JobObservation{JobUID: c.observedUID, Phase: "Running", PodsReady: true}, nil
}

func TestKueueACKBoundRecoveryIsReadOnlyAndChecksUIDAgainOnObservation(t *testing.T) {
	client := &boundInspectTestClient{fakeClient: &fakeClient{}, expectedUID: "bound-uid", observedUID: "bound-uid"}
	p := createACKProjection()
	p.ExpectedCreateUID = "bound-uid"
	p.OnBeforeCreate = nil
	p.OnCreateACK = nil
	got, err := NewProvider(client).ReconcileWorkload(context.Background(), p)
	if err != nil { t.Fatal(err) }
	if client.inspectCalls != 1 || client.order[0] != "inspect-bound:job-train-a" {
		t.Fatalf("bound Job was not inspected read-only: calls=%d order=%#v", client.inspectCalls, client.order)
	}
	for _, action := range client.order {
		if strings.HasPrefix(action, "job:") || strings.HasPrefix(action, "claim:") {
			t.Fatalf("ACK-bound recovery issued a write: %#v", client.order)
		}
	}
	if got.ObservedGeneration != p.Generation { t.Fatalf("generation changed: %#v", got) }
}

func TestKueueACKBoundRecoveryRejectsReplacedJobUID(t *testing.T) {
	client := &boundInspectTestClient{fakeClient: &fakeClient{}, expectedUID: "bound-uid", observedUID: "replacement-uid"}
	p := createACKProjection()
	p.ExpectedCreateUID = "bound-uid"
	p.OnBeforeCreate = nil
	p.OnCreateACK = nil
	_, err := NewProvider(client).ReconcileWorkload(context.Background(), p)
	if !errors.Is(err, verifiedops.ErrCreateUIDUnproven) { t.Fatalf("replacement UID was accepted: %v", err) }
	for _, action := range client.order {
		if strings.HasPrefix(action, "job:") || strings.HasPrefix(action, "claim:") {
			t.Fatalf("failed identity check still issued a write: %#v", client.order)
		}
	}
}
