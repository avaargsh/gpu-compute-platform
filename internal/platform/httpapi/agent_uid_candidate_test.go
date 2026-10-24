package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

type recordingCreateUIDStore struct {
	agentstore.Store
	calls int
	last  verifiedops.JobCreateUIDBinding
	err   error
}

func (s *recordingCreateUIDStore) RecordJobCreateUID(_ context.Context, in verifiedops.JobCreateUIDBinding) error {
	s.calls++
	s.last = in
	return s.err
}

func TestUIDCandidateSinkRejectsPlainHTTPAndCrossClusterSpoofing(t *testing.T) {
	store := &recordingCreateUIDStore{Store: agentstore.NewMemory()}
	handler := NewRouterWithAgentStore(store)
	const path = "/api/v1/agent/workload-create-uid"
	const body = `{"clusterId":"cluster-a","operationId":"11111111111111111111111111111111","projectId":"research","workloadId":"train","generation":1,"namespace":"research","jobName":"job-train","jobUid":"server-uid","jobResourceVersion":"11","leaseOwner":"worker-a"}`
	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, agentRequest(t, http.MethodPost, path, body, clusterAIdentity))
	if plain.Code != http.StatusUnauthorized || store.calls != 0 {
		t.Fatalf("unwrapped handler claimed Agent identity: status=%d calls=%d", plain.Code, store.calls)
	}
	protected, err := AgentMTLSRouter(handler, "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		identity string
		payload  string
		want     int
	}{
		{"no-cert", "", body, http.StatusUnauthorized},
		{"other-cluster-cert", "spiffe://platform.example/cluster/cluster-b/agent", body, http.StatusForbidden},
		{"duplicate-cluster-id", clusterAIdentity, strings.Replace(body, `"clusterId":"cluster-a"`, `"clusterId":"cluster-a","ClusterId":"cluster-b"`, 1), http.StatusBadRequest},
		{"wrong-operation-scope", clusterAIdentity, strings.Replace(body, `"clusterId":"cluster-a"`, `"clusterId":"cluster-b"`, 1), http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, agentRequest(t, http.MethodPost, path, test.payload, test.identity))
			if rec.Code != test.want || store.calls != 0 {
				t.Fatalf("status=%d want=%d calls=%d", rec.Code, test.want, store.calls)
			}
		})
	}
	ok := httptest.NewRecorder()
	protected.ServeHTTP(ok, agentRequest(t, http.MethodPost, path, body, clusterAIdentity))
	if ok.Code != http.StatusNoContent || store.calls != 1 ||
		store.last.OperationID != "11111111111111111111111111111111" ||
		string(store.last.ClusterID) != "cluster-a" || store.last.JobUID != "server-uid" {
		t.Fatalf("valid scoped UID candidate was rejected: status=%d calls=%d last=%#v", ok.Code, store.calls, store.last)
	}
}

func TestUIDCandidateSinkMapsStoreRejectionWithoutExposingIdentity(t *testing.T) {
	store := &recordingCreateUIDStore{Store: agentstore.NewMemory(), err: verifiedops.ErrCreateUIDConflict}
	handler, err := AgentMTLSRouter(NewRouterWithAgentStore(store), "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, agentRequest(t, http.MethodPost, "/api/v1/agent/workload-create-uid",
		`{"clusterId":"cluster-a","operationId":"11111111111111111111111111111111"}`, clusterAIdentity))
	if rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), "server-uid") {
		t.Fatalf("candidate conflict leaked provider evidence: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

type recordingCreateAttemptStore struct {
	*recordingCreateUIDStore
	attemptCalls int
	lastAttempt  verifiedops.JobCreateAttempt
	attemptErr   error
	resolveCalls int
	resolution   verifiedops.JobCreateAttemptResolution
	resolveErr   error
}

func (s *recordingCreateAttemptStore) PrepareJobCreateAttempt(_ context.Context, in verifiedops.JobCreateAttempt) error {
	s.attemptCalls++
	s.lastAttempt = in
	return s.attemptErr
}

func (s *recordingCreateAttemptStore) ResolveJobCreateAttempt(
	_ context.Context, _ string, _ domain.ID, _ domain.ID, _ int64,
) (verifiedops.JobCreateAttemptResolution, error) {
	s.resolveCalls++
	return s.resolution, s.resolveErr
}
func TestPreparedJobCreateRequiresVerifiedClusterCertificate(t *testing.T) {
	store := &recordingCreateAttemptStore{recordingCreateUIDStore: &recordingCreateUIDStore{Store: agentstore.NewMemory()}}
	routes := NewRouterWithAgentStore(store)
	body := `{"clusterId":"cluster-a","operationId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","projectId":"p","workloadId":"train","generation":1,"namespace":"research","jobName":"job-train","leaseOwner":"agent-a"}`
	path := "/api/v1/agent/workload-create-attempt"
	plain := httptest.NewRecorder()
	routes.ServeHTTP(plain, agentRequest(t, http.MethodPost, path, body, clusterAIdentity))
	if plain.Code != http.StatusUnauthorized || store.attemptCalls != 0 {
		t.Fatalf("unwrapped attempt sink: %d", plain.Code)
	}
	protected, err := AgentMTLSRouter(routes, "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		identity string
		content  string
		want     int
	}{
		{"missing-cert", "", body, http.StatusUnauthorized},
		{"other-cluster", "spiffe://platform.example/cluster/cluster-b/agent", body, http.StatusForbidden},
		{"body-spoof", clusterAIdentity, strings.Replace(body, `"clusterId":"cluster-a"`, `"clusterId":"cluster-b"`, 1), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, agentRequest(t, http.MethodPost, path, tc.content, tc.identity))
			if rec.Code != tc.want || store.attemptCalls != 0 {
				t.Fatalf("scope escaped: %d calls=%d", rec.Code, store.attemptCalls)
			}
		})
	}
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, agentRequest(t, http.MethodPost, path, body, clusterAIdentity))
	if rec.Code != http.StatusNoContent || store.attemptCalls != 1 || store.lastAttempt.OperationID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("verified Agent prepare failed: %d %#v", rec.Code, store.lastAttempt)
	}
	store.attemptErr = verifiedops.ErrCreateAttemptUnresolved
	again := httptest.NewRecorder()
	protected.ServeHTTP(again, agentRequest(t, http.MethodPost, path, body, clusterAIdentity))
	if again.Code != http.StatusConflict {
		t.Fatalf("ambiguous retry allowed: %d", again.Code)
	}
}

func TestResolveJobCreateAttemptRequiresVerifiedClusterMTLS(t *testing.T) {
	store := &recordingCreateAttemptStore{
		recordingCreateUIDStore: &recordingCreateUIDStore{Store: agentstore.NewMemory()},
		resolution: verifiedops.JobCreateAttemptResolution{
			State: verifiedops.JobCreateAttemptACKBound,
			Binding: &verifiedops.JobCreateUIDBinding{
				OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClusterID: "cluster-a",
				ProjectID: "project-a", WorkloadID: "train-a", Generation: 3,
				Namespace: "project-a", JobName: "job-train-a", JobUID: "apiserver-uid",
				JobResourceVersion: "42", LeaseOwner: "old-agent",
			},
		},
	}
	handler := NewRouterWithAgentStore(store)
	const path = "/api/v1/agent/workload-create-attempt?clusterId=cluster-a&operationId=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&workloadId=train-a&generation=3"
	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, agentRequest(t, http.MethodGet, path, "", clusterAIdentity))
	if plain.Code != http.StatusUnauthorized || store.resolveCalls != 0 {
		t.Fatalf("unwrapped CREATE resolution accepted: status=%d calls=%d", plain.Code, store.resolveCalls)
	}
	protected, err := AgentMTLSRouter(handler, "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, identity, path string
		want                 int
	}{
		{"missing-cert", "", path, http.StatusUnauthorized},
		{"wrong-cert-scope", "spiffe://platform.example/cluster/cluster-b/agent", path, http.StatusForbidden},
		{"duplicate-operation", clusterAIdentity, path + "&operationId=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, agentRequest(t, http.MethodGet, tc.path, "", tc.identity))
			if rec.Code != tc.want || store.resolveCalls != 0 {
				t.Fatalf("scope escaped: status=%d want=%d calls=%d", rec.Code, tc.want, store.resolveCalls)
			}
		})
	}
	valid := httptest.NewRecorder()
	protected.ServeHTTP(valid, agentRequest(t, http.MethodGet, path, "", clusterAIdentity))
	if valid.Code != http.StatusOK || valid.Header().Get("Cache-Control") != "no-store" || store.resolveCalls != 1 {
		t.Fatalf("valid mTLS resolution failed: status=%d cache=%q calls=%d body=%q", valid.Code, valid.Header().Get("Cache-Control"), store.resolveCalls, valid.Body.String())
	}
	var got verifiedops.JobCreateAttemptResolution
	if err := json.NewDecoder(valid.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != verifiedops.JobCreateAttemptACKBound || got.Binding == nil || got.Binding.JobUID != "apiserver-uid" {
		t.Fatalf("incomplete bound resolution: %#v", got)
	}
}
