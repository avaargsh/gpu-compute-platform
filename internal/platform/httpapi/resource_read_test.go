package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestGetWorkloadProjectsStatusWithoutExposingCluster(t *testing.T) {
	store := agentstore.NewMemory()
	bindings := NewMemoryPlacementResolver()
	bindings.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-h100", ClusterID: "cluster-a", Provider: "kueue"})
	if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
		Kind: "Workload", ID: "train-1", Generation: 8,
		Spec: map[string]any{"projectID": domain.ID("project-1"), "poolID": domain.ID("pool-h100"), "image": "example/train:v8"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Report(context.Background(), "cluster-a", []agent.Observation{{
		Kind: "Workload", ID: "train-1", ObservedGeneration: 7,
	}}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewRouterWithDependencies(store, bindings))
	defer server.Close()
	resp, err := server.Client().Get(server.URL + "/api/v1/workloads/train-1?poolId=pool-h100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got workloadView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status.SyncState != "Reconciling" || got.Status.DesiredGeneration != 8 || got.Status.ObservedGeneration != 7 {
		t.Fatalf("status=%#v", got.Status)
	}
	if got.Spec["image"] != "example/train:v8" {
		t.Fatalf("spec=%#v", got.Spec)
	}
}
