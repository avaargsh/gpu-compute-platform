package kueue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	"k8s.io/apimachinery/pkg/types"
)

type ackCaptureClient struct {
	*fakeClient
	response JobCreateResponse
	err error
	captures int
}

func (c *ackCaptureClient) ApplyJobWithCreateResponse(_ context.Context, _ Job) (JobCreateResponse, error) {
	c.captures++
	return c.response, c.err
}

func createACKProjection() baseprovider.WorkloadProjection {
	return baseprovider.WorkloadProjection{
		Provider: "kueue", ClusterID: "cluster-a", ProjectID: "project-a",
		PoolID: "pool-a", WorkloadID: "train-a", Namespace: "project-a",
		Generation: 1, OperationID: strings.Repeat("a", 32),
		Image: "example/train@sha256:" + strings.Repeat("b", 64),
		Accelerator: domain.AcceleratorRequest{Class:"h100", Quota:1},
		AcceleratorBinding: domain.AcceleratorBinding{Class:"h100", ResourceName:"nvidia.com/gpu", Flavor:"h100"},
	}
}

func TestKueueOnlyPublishesActualAPICreateACK(t *testing.T) {
	for _, tc := range []struct {
		name string
		disposition JobApplyDisposition
		wantCalls int
	}{
		{"created", JobCreateAcknowledged,1},
		{"existing", JobExistingUnbound,0},
		{"already-exists-race", JobRaceAdoptedUnbound,0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c:=&ackCaptureClient{fakeClient:&fakeClient{}, response:JobCreateResponse{
				Disposition:tc.disposition,UID:types.UID("returned-uid"),ResourceVersion:"17",
			}}
			p:=createACKProjection()
			calls:=0
			p.OnCreateACK=func(_ context.Context, ack baseprovider.JobCreateACK) error {
				calls++
				if ack.UID!="returned-uid"||ack.ResourceVersion!="17"||ack.Name!="job-train-a"||ack.Namespace!="project-a" {
					t.Fatalf("unexpected actual ACK: %#v",ack)
				}
				return nil
			}
			_,err:=NewProvider(c).ReconcileWorkload(context.Background(),p)
			if err!=nil||c.captures!=1||calls!=tc.wantCalls{
				t.Fatalf("decision %s: capture=%d publish=%d err=%v",tc.disposition,c.captures,calls,err)
			}
		})
	}
}

func TestKueuePublishFailureStopsObservationAndReportsRetryable(t *testing.T) {
	c:=&ackCaptureClient{fakeClient:&fakeClient{},response:JobCreateResponse{Disposition:JobCreateAcknowledged,UID:"uid",ResourceVersion:"9"}}
	p:=createACKProjection()
	p.OnCreateACK=func(context.Context,baseprovider.JobCreateACK) error{return errors.New("disconnected")}
	_,err:=NewProvider(c).ReconcileWorkload(context.Background(),p)
	if err==nil||!baseprovider.IsRetryable(err){t.Fatalf("persistence failure must retry: %v",err)}
	for _,operation:=range c.order {
		if strings.HasPrefix(operation,"job:"){t.Fatalf("fell back to a second non-proof CREATE: %#v",c.order)}
	}
}
func TestKueueCannotCreateWithClaimedOperationOnLegacyAdapter(t *testing.T){
	p:=createACKProjection()
	p.OnCreateACK=func(context.Context,baseprovider.JobCreateACK)error{return nil}
	_,err:=NewProvider(&fakeClient{}).ReconcileWorkload(context.Background(),p)
	if err==nil||!strings.Contains(err.Error(),"capture unavailable"){t.Fatalf("missing CREATE seam accepted: %v",err)}
}
func TestKueueTimeoutCannotPublishACK(t *testing.T){
	c:=&ackCaptureClient{fakeClient:&fakeClient{},err:errors.New("lost ACK")}
	p:=createACKProjection()
	calls:=0
	p.OnCreateACK=func(context.Context,baseprovider.JobCreateACK)error{calls++;return nil}
	_,err:=NewProvider(c).ReconcileWorkload(context.Background(),p)
	if err==nil||calls!=0{t.Fatalf("ambiguous CREATE became candidate proof: %v calls %d",err,calls)}
}
