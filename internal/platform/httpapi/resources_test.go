package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func boundRouter(store agentstore.Store, projectCluster, poolCluster domain.ID) http.Handler {
	placement := NewMemoryPlacementResolver()
	placement.BindProject(domain.ProjectBinding{ProjectID: "project-1", ClusterID: projectCluster, Namespace: "project-1"})
	placement.BindPool(domain.ClusterBinding{PoolID: "pool-h100", ClusterID: poolCluster, Provider: "kueue"})
	return NewRouterWithDependencies(store, placement)
}

func TestResourceAPIProjectsWorkloadDesiredState(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()

	body := []byte(`{
		"metadata":{"id":"train-1","name":"train-1","generation":7},
		"projectId":"project-1",
		"poolId":"pool-h100",
		"spec":{"image":"example/train:latest","command":["python","train.py"],"accelerator":{"class":"h100-80g","quota":2}},
		"status":{"observedGeneration":0}
	}`)
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	desired, err := store.Desired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(desired) != 1 {
		t.Fatalf("expected one desired resource: %#v", desired)
	}
	got := desired[0]
	if got.Kind != "Workload" || got.ID != "train-1" || got.Generation != 7 {
		t.Fatalf("unexpected desired identity: %#v", got)
	}
	if got.Spec["projectID"] != domain.ID("project-1") || got.Spec["poolID"] != domain.ID("pool-h100") || got.Spec["namespace"] != "project-1" {
		t.Fatalf("unexpected desired placement: %#v", got.Spec)
	}
}

func TestResourceAPIRejectsPathBodyIdentityMismatch(t *testing.T) {
	server := httptest.NewServer(boundRouter(agentstore.NewMemory(), "cluster-a", "cluster-a"))
	defer server.Close()
	body := []byte(`{"metadata":{"id":"other","generation":1},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/train:latest","accelerator":{"class":"h100-80g","quota":1}}}`)
	resp, err := http.Post(server.URL+"/unused", "application/json", nil)
	if err == nil {
		resp.Body.Close()
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for identity mismatch, got %d", resp.StatusCode)
	}
}

func TestResourceAPIRejectsCrossClusterBindings(t *testing.T) {
	server := httptest.NewServer(boundRouter(agentstore.NewMemory(), "cluster-a", "cluster-b"))
	defer server.Close()
	body := []byte(`{"metadata":{"id":"train-1","generation":1},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/train:latest","accelerator":{"class":"h100-80g","quota":1}}}`)
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for cross-cluster bindings, got %d", resp.StatusCode)
	}
}
