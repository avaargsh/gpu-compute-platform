package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

const devWriteTokenTest = "dev-write-token-must-be-independent-and-strong-1234"
const devReadTokenTest = "dev-read-token-must-be-independent-and-strong-12345"

const devPayload = `{"metadata":{"id":"train-test","generation":1},"projectId":"research","poolId":"pool-research","spec":{"image":"example/train:latest","command":["python","train.py"],"accelerator":{"class":"h100-80g","quota":1},"workspace":{"claimName":"training-data"}}}`

func developmentWriteFixture(t *testing.T) (*httptest.Server, *agentstore.Memory) {
	t.Helper()
	store := agentstore.NewMemory()
	placement := NewMemoryPlacementResolver()
	placement.BindProject(domain.ProjectBinding{Metadata: domain.Metadata{Generation: 1}, ProjectID: "research", ClusterID: "cluster-a", Namespace: "research-ns"})
	placement.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-research", ClusterID: "cluster-a", Provider: "kueue"})
	placement.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-foreign", ClusterID: "cluster-a", Provider: "kueue"})
	for _, p := range []struct{ name, project string }{{"pool-research", "research"}, {"pool-foreign", "foreign"}} {
		if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
			Kind: "ComputePool", ID: domain.ID(p.name), Generation: 1,
			Spec: map[string]any{
				"projectID": p.project, "namespace": "research-ns",
				"acceleratorBindings": []domain.AcceleratorBinding{{Class: "h100-80g", ResourceName: "nvidia.com/gpu", Flavor: "h100"}},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	router, err := NewRouterWithOperatorPreview(store, placement, OperatorPreview{
		Token: devReadTokenTest, DevelopmentWriteToken: devWriteTokenTest,
		ClusterID: "cluster-a", Projects: []string{"research"}, DevelopmentWrites: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(router), store
}

func putDevWorkload(t *testing.T, server *httptest.Server, path, body, read, write string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, server.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:3001")
	if read != "" {
		req.Header.Set("Authorization", "Bearer "+read)
	}
	if write != "" {
		req.Header.Set("X-Operator-Write-Token", write)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func TestDevWorkloadWriteRequiresBothTokensAndProjectScope(t *testing.T) {
	server, store := developmentWriteFixture(t)
	defer server.Close()
	path := "/api/v1/operator/clusters/cluster-a/projects/research/workloads/train-test"
	for _, input := range []struct{ read, write string }{
		{"", devWriteTokenTest},
		{devReadTokenTest, ""},
		{devReadTokenTest, "bad-bad-bad-bad-bad-bad-bad-bad-bad-bad"},
		{"bad-bad-bad-bad-bad-bad-bad-bad-bad-bad", devWriteTokenTest},
	} {
		if status := putDevWorkload(t, server, path, devPayload, input.read, input.write); status != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", status)
		}
	}
	if status := putDevWorkload(t, server, "/api/v1/operator/clusters/cluster-a/projects/foreign/workloads/train-test", devPayload, devReadTokenTest, devWriteTokenTest); status != http.StatusNotFound {
		t.Fatalf("foreign project status=%d", status)
	}
	if status := putDevWorkload(t, server, "/api/v1/operator/clusters/cluster-other/projects/research/workloads/train-test", devPayload, devReadTokenTest, devWriteTokenTest); status != http.StatusNotFound {
		t.Fatalf("foreign cluster status=%d", status)
	}
	_, exists, err := store.GetDesired(context.Background(), "cluster-a", "Workload", "train-test")
	if err != nil || exists {
		t.Fatalf("unauthorized Workload was created: found=%t err=%v", exists, err)
	}
}

func TestDevWorkloadWriteUsesExistingAtomicGoContract(t *testing.T) {
	server, store := developmentWriteFixture(t)
	defer server.Close()
	path := "/api/v1/operator/clusters/cluster-a/projects/research/workloads/train-test"
	if status := putDevWorkload(t, server, path, devPayload, devReadTokenTest, devWriteTokenTest); status != http.StatusNoContent {
		t.Fatalf("scoped create status=%d want 204", status)
	}
	actual, exists, err := store.GetDesired(context.Background(), "cluster-a", "Workload", "train-test")
	if err != nil || !exists || actual.Spec["projectID"] != domain.ID("research") ||
		actual.Spec["namespace"] != "research-ns" || actual.Spec["provider"] != "kueue" {
		t.Fatalf("Go desired creation contract failed: %+v found=%t err=%v", actual, exists, err)
	}
	if status := putDevWorkload(t, server, path, devPayload, devReadTokenTest, devWriteTokenTest); status != http.StatusNoContent {
		t.Fatalf("unchanged replay status=%d want 204", status)
	}
	changed := strings.Replace(devPayload, "example/train:latest", "example/different:latest", 1)
	if status := putDevWorkload(t, server, path, changed, devReadTokenTest, devWriteTokenTest); status != http.StatusConflict {
		t.Fatalf("same generation changed spec must fail 409, got %d", status)
	}
}

func TestDevWorkloadWriteRejectsForeignPoolAndAmbiguousScope(t *testing.T) {
	server, store := developmentWriteFixture(t)
	defer server.Close()
	path := "/api/v1/operator/clusters/cluster-a/projects/research/workloads/train-test"
	foreign := strings.Replace(devPayload, "pool-research", "pool-foreign", 1)
	if status := putDevWorkload(t, server, path, foreign, devReadTokenTest, devWriteTokenTest); status != http.StatusConflict {
		t.Fatalf("same-cluster foreign project pool accepted: HTTP %d", status)
	}
	mismatched := strings.Replace(devPayload, `"projectId":"research"`, `"projectId":"foreign"`, 1)
	if status := putDevWorkload(t, server, path, mismatched, devReadTokenTest, devWriteTokenTest); status != http.StatusBadRequest {
		t.Fatalf("mismatched body project accepted: HTTP %d", status)
	}
	multiJSON := devPayload + devPayload
	if status := putDevWorkload(t, server, path, multiJSON, devReadTokenTest, devWriteTokenTest); status != http.StatusBadRequest {
		t.Fatalf("multiple JSON objects accepted: HTTP %d", status)
	}
	_, exists, err := store.GetDesired(context.Background(), "cluster-a", "Workload", "train-test")
	if err != nil || exists {
		t.Fatalf("rejected Write changed desired: exists=%v err=%v", exists, err)
	}
}

func TestDevWorkloadWriteRejectsCrossSiteAndOversize(t *testing.T) {
	server, _ := developmentWriteFixture(t)
	defer server.Close()
	path := "/api/v1/operator/clusters/cluster-a/projects/research/workloads/train-test"
	req, _ := http.NewRequest(http.MethodPut, server.URL+path, bytes.NewBufferString(devPayload))
	req.Header.Set("Authorization", "Bearer "+devReadTokenTest)
	req.Header.Set("X-Operator-Write-Token", devWriteTokenTest)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://example.com")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign browser origin accepted: HTTP %d", resp.StatusCode)
	}
	oversized := strings.Repeat("x", maxOperatorDevWorkloadBytes+1)
	if status := putDevWorkload(t, server, path, oversized, devReadTokenTest, devWriteTokenTest); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize request status=%d", status)
	}
}

func TestDevWorkloadWriteDisabledUnlessExplicitDevelopmentMode(t *testing.T) {
	store := agentstore.NewMemory()
	bindings := NewMemoryPlacementResolver()
	for _, cfg := range []OperatorPreview{
		{Token: devReadTokenTest, DevelopmentWriteToken: devWriteTokenTest,
			ClusterID: "cluster-a", Projects: []string{"research"}},
		{Token: devReadTokenTest, DevelopmentWriteToken: devReadTokenTest,
			ClusterID: "cluster-a", Projects: []string{"research"}, DevelopmentWrites: true},
		{Token: devReadTokenTest, DevelopmentWriteToken: "short",
			ClusterID: "cluster-a", Projects: []string{"research"}, DevelopmentWrites: true},
	} {
		if _, err := NewRouterWithOperatorPreview(store, bindings, cfg); err == nil {
			t.Fatalf("accepted unsafe local write config: %+v", cfg)
		}
	}
}
