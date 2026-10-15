package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

const previewTestToken = "test-token-of-at-least-thirty-two-safe-bytes-123456789"

func fixtureOperatorPreview(t *testing.T) (*httptest.Server, *agentstore.Memory) {
	t.Helper()
	store := agentstore.NewMemory()
	ctx := context.Background()
	err := store.Register(ctx, agent.Registration{ClusterID: "cluster-a", AgentVersion: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []agent.DesiredResource{
		{Kind: "Workload", ID: "work-a", Generation: 4, Spec: map[string]any{"projectID": domain.ID("allowed")}},
		{Kind: "ComputePool", ID: "pool-a", Generation: 1, Spec: map[string]any{"projectID": "allowed"}},
		{Kind: "Workload", ID: "work-b", Generation: 2, Spec: map[string]any{"projectID": "allowed"}},
		{Kind: "Workload", ID: "secret-foreign", Generation: 1, Spec: map[string]any{"projectID": "foreign"}},
		{Kind: "Workload", ID: "secret-unknown", Generation: 1, Spec: map[string]any{"projectID": 22}},
	} {
		if err := store.UpsertDesired(ctx, "cluster-a", item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Report(ctx, "cluster-a", []agent.Observation{
		{Kind: "Workload", ID: "work-a", ObservedGeneration: 4, EvidenceRefs: []string{"job/work-a"}},
	}); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouterWithOperatorPreview(store, NewMemoryPlacementResolver(), OperatorPreview{
		Token: previewTestToken, ClusterID: "cluster-a", Projects: []string{"allowed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(router), store
}

func callPreview(t *testing.T, client *http.Client, endpoint, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestOperatorPreviewRejectsAnonymousWrongTokenAndForeignCluster(t *testing.T) {
	server, _ := fixtureOperatorPreview(t)
	defer server.Close()
	endpoint := server.URL + "/api/v1/operator/clusters/cluster-a/snapshot"
	for _, token := range []string{"", "not-the-operator-token", previewTestToken + "-other"} {
		resp := callPreview(t, server.Client(), endpoint, token)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token=%q status=%d, want 401", token, resp.StatusCode)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("sensitive operator response must not be cached")
		}
	}
	resp := callPreview(t, server.Client(), server.URL+"/api/v1/operator/clusters/cluster-foreign/snapshot", previewTestToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign cluster status=%d, want 404", resp.StatusCode)
	}
}

func TestOperatorPreviewScopesBothCountAndRows(t *testing.T) {
	server, _ := fixtureOperatorPreview(t)
	defer server.Close()
	resp := callPreview(t, server.Client(), server.URL+"/api/v1/operator/clusters/cluster-a/snapshot?limit=120", previewTestToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status=%d", resp.StatusCode)
	}
	var data operatorSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 3 || len(data.Rows) != 3 || data.Truncated || data.NextCursor != "" {
		t.Fatalf("unexpected scoped snapshot: total=%d rows=%d truncated=%t", data.Total, len(data.Rows), data.Truncated)
	}
	for _, row := range data.Rows {
		if projectScope(row.Desired.Spec) != "allowed" || strings.HasPrefix(string(row.Desired.ID), "secret-") {
			t.Fatalf("foreign row exposed: %#v", row)
		}
	}
	if data.Rows[0].Desired.ID != "pool-a" || data.Rows[1].Desired.ID != "work-a" {
		t.Fatalf("unstable ordering: %#v", data.Rows)
	}
	if data.Rows[1].State == nil || data.Rows[1].State.SyncState != "Synced" {
		t.Fatalf("independent observation missing: %#v", data.Rows[1])
	}
	// Synced is generation agreement only; no Serving Ready claim.
}

func TestOperatorPreviewSignedPaginationAndTampering(t *testing.T) {
	server, _ := fixtureOperatorPreview(t)
	defer server.Close()
	base := server.URL + "/api/v1/operator/clusters/cluster-a/snapshot?limit=1"
	all := make([]string, 0)
	cursor := ""
	for page := 0; page < 3; page++ {
		endpoint := base
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}
		resp := callPreview(t, server.Client(), endpoint, previewTestToken)
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("page=%d status=%d", page, resp.StatusCode)
		}
		var data operatorSnapshot
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		if len(data.Rows) != 1 || data.Total != 3 {
			t.Fatalf("page %d: %#v", page, data)
		}
		all = append(all, string(data.Rows[0].Desired.ID))
		cursor = data.NextCursor
	}
	if strings.Join(all, ",") != "pool-a,work-a,work-b" || cursor != "" {
		t.Fatalf("pagination got=%v next=%q", all, cursor)
	}
	for _, bad := range []string{"invalid", "!!!", strings.Repeat("A", 1200)} {
		resp := callPreview(t, server.Client(), base+"&cursor="+url.QueryEscape(bad), previewTestToken)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("bad cursor %q was accepted: %d", bad, resp.StatusCode)
		}
	}
}

func TestOperatorPreviewNeverEnablesMutations(t *testing.T) {
	server, _ := fixtureOperatorPreview(t)
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/operator/clusters/cluster-a/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+previewTestToken)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("operator mutation status=%d, want 405", resp.StatusCode)
	}
}

func TestOperatorPreviewRequiresExplicitCompleteScope(t *testing.T) {
	store := agentstore.NewMemory()
	for _, cfg := range []OperatorPreview{
		{},
		{Token: "weak", ClusterID: "a", Projects: []string{"p"}},
		{Token: previewTestToken, Projects: []string{"p"}},
		{Token: previewTestToken, ClusterID: "a", Projects: []string{""}},
		{Token: previewTestToken, ClusterID: "a", Projects: []string{" p"}},
	} {
		if _, err := NewOperatorPreviewHandler(store, cfg); err == nil {
			t.Fatalf("invalid cfg accepted: %#v", cfg)
		}
	}
}

func TestOperatorPreviewRejectsCrossOriginAndFormAgentWrites(t *testing.T) {
	server, store := fixtureOperatorPreview(t)
	defer server.Close()

	const forged = `{"clusterId":"injected","agentVersion":"forged","kubernetesVersion":"v0"}`
	tests := []struct {
		name        string
		origin      string
		contentType string
		fetchSite   string
		wantStatus  int
	}{
		{name: "cross origin JSON", origin: "https://attacker.example", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "cross site fetch metadata", contentType: "application/json", fetchSite: "cross-site", wantStatus: http.StatusForbidden},
		{name: "simple browser form", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "HTML form", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType},
		{name: "missing media type", wantStatus: http.StatusUnsupportedMediaType},
		{name: "machine JSON still supported", contentType: "application/json", wantStatus: http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/agent/register", strings.NewReader(forged))
			if err != nil {
				t.Fatal(err)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d, want=%d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusNoContent {
				if _, found, err := store.GetAgentStatus(context.Background(), "injected"); err != nil || found {
					t.Fatalf("blocked mutation changed Agent store: found=%t err=%v", found, err)
				}
			}
		})
	}
}

func TestOperatorPreviewPaginationRejectsDesiredChangeEvenAtSameCount(t *testing.T) {
	server, store := fixtureOperatorPreview(t)
	defer server.Close()
	base := server.URL + "/api/v1/operator/clusters/cluster-a/snapshot?limit=1"

	resp := callPreview(t, server.Client(), base, previewTestToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial page status=%d", resp.StatusCode)
	}
	var first operatorSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if first.Total != 3 || first.NextCursor == "" {
		t.Fatalf("expected three scoped records and cursor: %#v", first)
	}

	// Changing foreign project state must not disrupt an authorized reader.
	if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
		Kind: "Workload", ID: "secret-foreign", Generation: 2,
		Spec: map[string]any{"projectID": "foreign"},
	}); err != nil {
		t.Fatal(err)
	}
	urlWithCursor := base + "&cursor=" + url.QueryEscape(first.NextCursor)
	foreignResp := callPreview(t, server.Client(), urlWithCursor, previewTestToken)
	foreignResp.Body.Close()
	if foreignResp.StatusCode != http.StatusOK {
		t.Fatalf("foreign update invalidated scoped cursor: %d", foreignResp.StatusCode)
	}

	// An update that preserves authorized Total MUST still fail closed.
	if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
		Kind: "Workload", ID: "work-b", Generation: 3,
		Spec: map[string]any{"projectID": "allowed"},
	}); err != nil {
		t.Fatal(err)
	}
	stale := callPreview(t, server.Client(), urlWithCursor, previewTestToken)
	defer stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale same-count cursor status=%d, want 409", stale.StatusCode)
	}
	if stale.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("stale response must not be cached")
	}
}

func TestOperatorPreviewInvalidatesCursorOnScopedReassignment(t *testing.T) {
	server, store := fixtureOperatorPreview(t)
	defer server.Close()
	base := server.URL + "/api/v1/operator/clusters/cluster-a/snapshot?limit=1"

	resp := callPreview(t, server.Client(), base, previewTestToken)
	var first operatorSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
		Kind: "Workload", ID: "work-b", Generation: 3,
		Spec: map[string]any{"projectID": "foreign"},
	}); err != nil {
		t.Fatal(err)
	}
	stale := callPreview(t, server.Client(), base+"&cursor="+url.QueryEscape(first.NextCursor), previewTestToken)
	stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("project reassignment must invalidate cursor, got %d", stale.StatusCode)
	}
	fresh := callPreview(t, server.Client(), base, previewTestToken)
	defer fresh.Body.Close()
	var after operatorSnapshot
	if err := json.NewDecoder(fresh.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if after.Total != 2 {
		t.Fatalf("foreign reassignment still in authorized count: %d", after.Total)
	}
}
