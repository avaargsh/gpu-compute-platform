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

func TestResourceAPIProjectsWorkloadDesiredState(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()

	body := []byte(`{
		"clusterId":"cluster-a",
		"namespace":"project-1",
		"resource":{
			"metadata":{"id":"train-1","name":"train-1","generation":7,"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z"},
			"projectId":"project-1",
			"poolId":"pool-h100",
			"spec":{"image":"example/train:latest","command":["python","train.py"],"accelerator":{"class":"h100-80g","quota":2}},
			"status":{"observedGeneration":0}
		}
	}`)
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	desired, err := store.Desired(context.Background(), domain.ID("cluster-a"))
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
	server := httptest.NewServer(NewRouterWithAgentStore(agentstore.NewMemory()))
	defer server.Close()

	body := []byte(`{
		"clusterId":"cluster-a",
		"namespace":"project-1",
		"resource":{
			"metadata":{"id":"other","generation":1,"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z"},
			"projectId":"project-1",
			"poolId":"pool-h100",
			"spec":{"image":"example/train:latest","accelerator":{"class":"h100-80g","quota":1}},
			"status":{"observedGeneration":0}
		}
	}`)
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for identity mismatch, got %d", resp.StatusCode)
	}
}
