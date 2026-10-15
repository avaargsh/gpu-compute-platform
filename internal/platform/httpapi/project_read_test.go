package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/authn"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type verifiedUser struct{}

func (verifiedUser) Verify(_ context.Context, token string) (authn.Principal, error) {
	if token == "valid-user-token" {
		return authn.Principal{Issuer: "https://issuer.example", Subject: "user-a"}, nil
	}
	if token == "foreign-issuer" {
		return authn.Principal{Issuer: "https://foreign.example", Subject: "user-a"}, nil
	}
	return authn.Principal{}, errors.New("not verified")
}

type testMembership struct {
	deny   bool
	outage bool
}

func (m testMembership) CanReadProject(_ context.Context, p authn.Principal, project domain.ID) (bool, error) {
	if m.outage {
		return false, errors.New("membership backend outage")
	}
	return !m.deny && p.Issuer == "https://issuer.example" && p.Subject == "user-a" && project == "p-a", nil
}
func fixtureProjectRead(t *testing.T, membership testMembership) http.Handler {
	t.Helper()
	store := agentstore.NewMemory()
	placements := NewMemoryPlacementResolver()
	for _, id := range []domain.ID{"p-a", "p-b"} {
		placements.BindProject(domain.ProjectBinding{Metadata: domain.Metadata{Generation: 1}, ProjectID: id, ClusterID: "cluster-1", Namespace: "ns-" + string(id)})
	}
	for _, id := range []domain.ID{"pool-a", "pool-b"} {
		placements.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: id, ClusterID: "cluster-1", Provider: "kueue"})
	}
	for _, in := range []agent.DesiredResource{
		{Kind: "ComputePool", ID: "pool-a", Generation: 2, Spec: map[string]any{"projectID": domain.ID("p-a")}},
		{Kind: "ComputePool", ID: "pool-b", Generation: 3, Spec: map[string]any{"projectID": domain.ID("p-b")}},
		{Kind: "Workload", ID: "job-a1", Generation: 4, Spec: map[string]any{"projectID": domain.ID("p-a"), "poolID": domain.ID("pool-a"), "image": "workload:v1"}},
		{Kind: "Workload", ID: "job-a2", Generation: 5, Spec: map[string]any{"projectID": domain.ID("p-a"), "poolID": domain.ID("pool-a")}},
		{Kind: "Workload", ID: "job-b1", Generation: 6, Spec: map[string]any{"projectID": domain.ID("p-b"), "poolID": domain.ID("pool-b"), "image": "foreign:v1"}},
	} {
		if err := store.UpsertDesired(context.Background(), "cluster-1", in); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Report(context.Background(), "cluster-1", []agent.Observation{
		{Kind: "Workload", ID: "job-a1", ObservedGeneration: 4, EvidenceRefs: []string{"job/job-a1"}},
	}); err != nil {
		t.Fatal(err)
	}
	router, err := NewProjectReadRouter(store, placements, membership, verifiedUser{})
	if err != nil {
		t.Fatal(err)
	}
	return router
}
func callRead(router http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func TestProjectReadAuthorizationDenyFirst(t *testing.T) {
	router := fixtureProjectRead(t, testMembership{})
	for _, tc := range []struct {
		path, token string
		want        int
	}{
		{"/api/v1/projects/p-a/workloads/job-a1", "", 401},
		{"/api/v1/projects/p-a/workloads/job-a1", "unverified", 401},
		{"/api/v1/projects/p-a/workloads/job-a1", "foreign-issuer", 404},
		{"/api/v1/projects/p-b/workloads/job-b1", "valid-user-token", 404},
		{"/api/v1/projects/p-a/workloads/job-b1", "valid-user-token", 404},
		{"/api/v1/projects/p-a/compute-pools/pool-b", "valid-user-token", 404},
		{"/api/v1/projects/p-a/workloads/job-a1", "valid-user-token", 200},
		{"/api/v1/projects/p-a/compute-pools/pool-a", "valid-user-token", 200},
		{"/api/v1/projects/p-a/workloads/absent", "valid-user-token", 404},
		{"/api/v1/agent/desired?clusterId=cluster-1", "valid-user-token", 404},
		{"/api/v1/internal/clusters/cluster-1/desired/Workload/job-a1", "valid-user-token", 404},
		{"/api/v1/workloads/job-a1", "valid-user-token", 404},
	} {
		rec := callRead(router, tc.path, tc.token)
		if rec.Code != tc.want {
			t.Errorf("%s token=%q status=%d want=%d body=%s", tc.path, tc.token, rec.Code, tc.want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "foreign:v1") {
			t.Fatal("foreign image leaked")
		}
	}
}

func TestProjectReadForbidsAllWrites(t *testing.T) {
	router := fixtureProjectRead(t, testMembership{})
	for _, method := range []string{"PUT", "POST", "PATCH", "DELETE"} {
		req := httptest.NewRequest(method, "/api/v1/projects/p-a/workloads/job-a1", strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer valid-user-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == 200 || rec.Code == 201 || rec.Code == 204 {
			t.Fatalf("%s write was allowed", method)
		}
	}
}

func TestProjectReadPaginationNeverCrossesProject(t *testing.T) {
	router := fixtureProjectRead(t, testMembership{})
	first := callRead(router, "/api/v1/projects/p-a/workloads?pageSize=1", "valid-user-token")
	if first.Code != 200 {
		t.Fatalf("first page=%d %s", first.Code, first.Body.String())
	}
	var result struct {
		Items []struct {
			Metadata domain.Metadata `json:"metadata"`
			Status   resourceState   `json:"status"`
		}
		Next string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Metadata.ID != "job-a1" || result.Items[0].Status.SyncState != "Synced" || result.Next == "" {
		t.Fatalf("unexpected first page: %#v", result)
	}
	second := callRead(router, "/api/v1/projects/p-a/workloads?pageSize=1&pageToken="+url.QueryEscape(result.Next), "valid-user-token")
	if second.Code != 200 {
		t.Fatalf("second page=%d %s", second.Code, second.Body.String())
	}
	var next struct {
		Items []struct {
			Metadata domain.Metadata `json:"metadata"`
		}
		Next string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].Metadata.ID != "job-a2" || next.Next != "" {
		t.Fatalf("unexpected second page: %#v", next)
	}
	for _, path := range []string{
		"/api/v1/projects/p-b/workloads?pageToken=" + url.QueryEscape(result.Next),
		"/api/v1/projects/p-a/compute-pools?pageToken=" + url.QueryEscape(result.Next),
		"/api/v1/projects/p-a/workloads?pageSize=0",
		"/api/v1/projects/p-a/workloads?pageSize=101",
		"/api/v1/projects/p-a/workloads?pageSize=1&pageSize=100",
	} {
		got := callRead(router, path, "valid-user-token")
		if path == "/api/v1/projects/p-b/workloads?pageToken="+url.QueryEscape(result.Next) {
			if got.Code != 404 {
				t.Fatalf("foreign project page status=%d", got.Code)
			}
		} else if got.Code != 400 {
			t.Errorf("bad pagination %s returned %d", path, got.Code)
		}
	}
	list := callRead(router, "/api/v1/projects/p-a/compute-pools", "valid-user-token")
	if list.Code != 200 || strings.Contains(list.Body.String(), "pool-b") {
		t.Fatalf("cross-project pool listing %s", list.Body.String())
	}
}

func TestProjectReadFailsClosedWhenMembershipStoreUnavailable(t *testing.T) {
	router := fixtureProjectRead(t, testMembership{outage: true})
	rec := callRead(router, "/api/v1/projects/p-a/workloads/job-a1", "valid-user-token")
	if rec.Code != 503 {
		t.Fatalf("store failure allowed: %d", rec.Code)
	}
}

func TestProjectReadRejectsHeaderSpoofingAndDuplicateAuthorization(t *testing.T) {
	router := fixtureProjectRead(t, testMembership{})
	req := httptest.NewRequest("GET", "/api/v1/projects/p-a/workloads/job-a1", nil)
	req.Header.Set("X-Forwarded-User", "user-a")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("forged header accepted: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/v1/projects/p-a/workloads/job-a1", nil)
	req.Header.Add("Authorization", "Bearer valid-user-token")
	req.Header.Add("Authorization", "Bearer invalid")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("duplicate bearer accepted: %d", rec.Code)
	}
}
