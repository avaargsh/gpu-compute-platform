package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	postgresstore "github.com/avaargsh/gpu-compute-platform/internal/store/postgres"
)

type recordingCreateUIDStore struct {
	agentstore.Store
	calls int
	last  postgresstore.JobCreateUIDBinding
	err   error
}

func (s *recordingCreateUIDStore) RecordJobCreateUID(_ context.Context, in postgresstore.JobCreateUIDBinding) error {
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
	}{
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
	store := &recordingCreateUIDStore{Store: agentstore.NewMemory(), err: postgresstore.ErrCreateUIDConflict}
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
