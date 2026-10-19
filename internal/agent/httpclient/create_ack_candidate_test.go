package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

func TestCreateUIDCandidateClientPostsScopedJSON(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Method != "POST" || r.URL.Path != "/api/v1/agent/workload-create-uid" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var in agent.JobCreateUIDCandidate
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.ClusterID != "cluster-a" || in.OperationID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
			in.JobUID != "uid" || in.LeaseOwner != "agent-a" {
			t.Errorf("candidate changed: %#v", in)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := New(server.URL, server.Client())
	err := client.PublishJobCreateUID(context.Background(), agent.JobCreateUIDCandidate{
		ClusterID: "cluster-a", OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProjectID: "research", WorkloadID: "job-a", Generation: 1,
		Namespace: "research", JobName: "job-job-a", JobUID: "uid", JobResourceVersion: "1", LeaseOwner: "agent-a",
	})
	if err != nil || count != 1 {
		t.Fatalf("publication failed: %v count=%d", err, count)
	}
}
func TestCreateUIDCandidateClientRejectsRedirect(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls++
		w.WriteHeader(204)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c := New(origin.URL, origin.Client())
	if err := c.PublishJobCreateUID(context.Background(), agent.JobCreateUIDCandidate{ClusterID: "cluster-a"}); err == nil || targetCalls != 0 {
		t.Fatalf("client followed unsafe authenticated POST redirect: calls=%d", targetCalls)
	}
}

func TestCreateAttemptClientUsesNonRedirectingAgentTransport(t *testing.T) {
	count:=0
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		count++
		if r.URL.Path!="/api/v1/agent/workload-create-attempt"{t.Errorf("unexpected attempt route %s",r.URL.Path)}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c:=New(server.URL,server.Client())
	if err:=c.PrepareJobCreateAttempt(context.Background(),verifiedops.JobCreateAttempt{
		ClusterID:"cluster-a",OperationID:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProjectID:"p",WorkloadID:"train",Generation:1,Namespace:"research",JobName:"job-train",LeaseOwner:"agent-a",
	});err!=nil||count!=1{t.Fatalf("failed attempt publication: %v count=%d",err,count)}
}
