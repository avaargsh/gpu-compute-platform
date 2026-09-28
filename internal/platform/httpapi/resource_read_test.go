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

func TestGetWorkloadRejectsWrongPool(t *testing.T) {
	store := agentstore.NewMemory()
	bindings := NewMemoryPlacementResolver()
	bindings.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-h100", ClusterID: "cluster-a", Provider: "kueue"})
	bindings.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-other", ClusterID: "cluster-a", Provider: "kueue"})
	if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
		Kind: "Workload", ID: "train-1", Generation: 1,
		Spec: map[string]any{"poolID": domain.ID("pool-h100")},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(store, bindings))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/v1/workloads/train-1?poolId=pool-other")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestGetComputePoolProjectsStatus(t *testing.T) {
	store := agentstore.NewMemory()
	bindings := NewMemoryPlacementResolver()
	bindings.BindPool(domain.ClusterBinding{Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-h100", ClusterID: "cluster-a", Provider: "kueue"})
	if err := store.UpsertDesired(context.Background(), "cluster-a", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-h100", Generation: 4,
		Spec: map[string]any{"projectID": domain.ID("project-1"), "scheduling": map[string]any{"mode": "default"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Report(context.Background(), "cluster-a", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-h100", ObservedGeneration: 4,
		EvidenceRefs: []string{"localqueue/pool-h100"},
	}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(store, bindings))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/v1/compute-pools/pool-h100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got computePoolView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status.SyncState != "Synced" || got.Status.DesiredGeneration != 4 || got.Status.ObservedGeneration != 4 {
		t.Fatalf("status=%#v", got.Status)
	}
	if len(got.Status.EvidenceRefs) != 1 || got.Status.EvidenceRefs[0] != "localqueue/pool-h100" {
		t.Fatalf("evidence=%#v", got.Status.EvidenceRefs)
	}
}
