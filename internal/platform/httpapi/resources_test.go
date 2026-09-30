package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func boundRouter(store agentstore.Store, projectCluster, poolCluster domain.ID) http.Handler {
	placement := NewMemoryPlacementResolver()
	placement.BindProject(domain.ProjectBinding{ProjectID: "project-1", ClusterID: projectCluster, Namespace: "project-1"})
	placement.BindPool(domain.ClusterBinding{PoolID: "pool-h100", ClusterID: poolCluster, Provider: "kueue"})
	if err := store.UpsertDesired(context.Background(), poolCluster, agent.DesiredResource{
		Kind:       "ComputePool",
		ID:         "pool-h100",
		Generation: 1,
		Spec: map[string]any{
			"projectID": "project-1",
			"namespace": "project-1",
			"acceleratorBindings": []domain.AcceleratorBinding{{
				Class:        "h100-80g",
				ResourceName: "nvidia.com/gpu",
				Flavor:       "h100",
			}},
		},
	}); err != nil {
		panic(err)
	}
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

	got, found, err := store.GetDesired(
		context.Background(),
		"cluster-a",
		"Workload",
		"train-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Generation != 7 {
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
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
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

func TestResourceAPIRejectsStaleWorkloadGenerationWithoutRollback(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()

	put := func(body string) int {
		req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := put(`{"metadata":{"id":"train-1","generation":8},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/v8","accelerator":{"class":"h100-80g","quota":2}}}`); got != http.StatusNoContent {
		t.Fatalf("generation 8 status=%d", got)
	}
	if got := put(`{"metadata":{"id":"train-1","generation":7},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/v7","accelerator":{"class":"h100-80g","quota":1}}}`); got != http.StatusConflict {
		t.Fatalf("stale generation status=%d, want 409", got)
	}

	desired, found, err := store.GetDesired(
		context.Background(),
		"cluster-a",
		"Workload",
		"train-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || desired.Generation != 8 || desired.Spec["image"] != "example/v8" {
		t.Fatalf("stale write rolled desired state back: %#v", desired)
	}
}

func TestBindingAPIRejectsStaleGenerationWithoutRollback(t *testing.T) {
	store := agentstore.NewMemory()
	bindings := NewMemoryPlacementResolver()
	server := httptest.NewServer(NewRouterWithDependencies(store, bindings))
	defer server.Close()

	put := func(body string) int {
		req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/projects/project-1/binding", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := put(`{"metadata":{"id":"binding","generation":8},"projectId":"project-1","clusterId":"cluster-a","namespace":"project-v8"}`); got != http.StatusNoContent {
		t.Fatalf("generation 8 status=%d", got)
	}
	if got := put(`{"metadata":{"id":"binding","generation":7},"projectId":"project-1","clusterId":"cluster-b","namespace":"project-v7"}`); got != http.StatusConflict {
		t.Fatalf("stale generation status=%d, want 409", got)
	}

	placement, err := bindings.ResolveProject(context.Background(), "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if placement.ClusterID != "cluster-a" || placement.Namespace != "project-v8" {
		t.Fatalf("stale binding rolled placement back: %#v", placement)
	}
}

func TestResourceAPIRequiresDeleteRecreateForWorkloadChanges(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()

	put := func(body string) int {
		req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-1", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	generationOne := `{"metadata":{"id":"train-1","generation":1},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/v1","command":["run"],"accelerator":{"class":"h100-80g","quota":1}}}`
	if got := put(generationOne); got != http.StatusNoContent {
		t.Fatalf("initial create status=%d", got)
	}
	if got := put(generationOne); got != http.StatusNoContent {
		t.Fatalf("identical replay status=%d, want 204", got)
	}

	sameGenerationMutation := `{"metadata":{"id":"train-1","generation":1},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/v2","command":["run"],"accelerator":{"class":"h100-80g","quota":1}}}`
	if got := put(sameGenerationMutation); got != http.StatusConflict {
		t.Fatalf("same-generation mutation status=%d, want 409", got)
	}

	nextGeneration := `{"metadata":{"id":"train-1","generation":2},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/v2","command":["run"],"accelerator":{"class":"h100-80g","quota":1}}}`
	if got := put(nextGeneration); got != http.StatusConflict {
		t.Fatalf("active generation replacement status=%d, want 409", got)
	}

	desired, found, err := store.GetDesired(context.Background(), "cluster-a", "Workload", "train-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || desired.Generation != 1 || desired.Spec["image"] != "example/v1" {
		t.Fatalf("active desired state drifted: %#v", desired)
	}

	if err := store.MarkDesiredDeleting(context.Background(), "cluster-a", "Workload", "train-1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := put(nextGeneration); got != http.StatusConflict {
		t.Fatalf("update during deletion status=%d, want 409", got)
	}
	if err := store.FinalizeDesired(context.Background(), "cluster-a", "Workload", "train-1", 1); err != nil {
		t.Fatal(err)
	}

	if got := put(nextGeneration); got != http.StatusNoContent {
		t.Fatalf("recreate after finalization status=%d, want 204", got)
	}
	desired, found, err = store.GetDesired(context.Background(), "cluster-a", "Workload", "train-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || desired.Generation != 2 || desired.Spec["image"] != "example/v2" {
		t.Fatalf("recreated desired state=%#v", desired)
	}
}

func TestResourceAPIRejectsWorkloadWhenPoolDesiredStateIsMissing(t *testing.T) {
	store := agentstore.NewMemory()
	placement := NewMemoryPlacementResolver()
	placement.BindProject(domain.ProjectBinding{
		ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "project-1",
	})
	placement.BindPool(domain.ClusterBinding{
		PoolID: "pool-h100", ClusterID: "cluster-a", Provider: "kueue",
	})
	server := httptest.NewServer(NewRouterWithDependencies(store, placement))
	defer server.Close()

	body := []byte(`{
		"metadata":{"id":"train-1","generation":1},
		"projectId":"project-1",
		"poolId":"pool-h100",
		"spec":{"image":"example/train:latest","accelerator":{"class":"h100-80g","quota":1}}
	}`)
	resp, err := server.Client().Do(mustRequest(
		t,
		http.MethodPut,
		server.URL+"/api/v1/workloads/train-1",
		body,
	))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
}

func TestResourceAPIRejectsWorkloadWithUnboundAcceleratorClass(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()

	body := []byte(`{
		"metadata":{"id":"train-1","generation":1},
		"projectId":"project-1",
		"poolId":"pool-h100",
		"spec":{"image":"example/train:latest","accelerator":{"class":"b200","quota":1}}
	}`)
	resp, err := server.Client().Do(mustRequest(
		t,
		http.MethodPut,
		server.URL+"/api/v1/workloads/train-1",
		body,
	))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
}

func mustRequest(t *testing.T, method, url string, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestResourceAPIPreservesIdenticalReplayAfterPoolDeletionStarts(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()

	body := []byte(`{
		"metadata":{"id":"train-replay","generation":1},
		"projectId":"project-1",
		"poolId":"pool-h100",
		"spec":{"image":"example/train:v1","accelerator":{"class":"h100-80g","quota":1}}
	}`)
	put := func(resourceID string, payload []byte) int {
		resp, err := server.Client().Do(mustRequest(
			t,
			http.MethodPut,
			server.URL+"/api/v1/workloads/"+resourceID,
			payload,
		))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := put("train-replay", body); got != http.StatusNoContent {
		t.Fatalf("initial create status=%d", got)
	}
	if err := store.MarkDesiredDeleting(
		context.Background(),
		"cluster-a",
		"ComputePool",
		"pool-h100",
		time.Now(),
	); err != nil {
		t.Fatal(err)
	}

	if got := put("train-replay", body); got != http.StatusNoContent {
		t.Fatalf("lost-ACK replay status=%d, want 204", got)
	}

	newBody := []byte(`{
		"metadata":{"id":"train-new","generation":1},
		"projectId":"project-1",
		"poolId":"pool-h100",
		"spec":{"image":"example/train:v1","accelerator":{"class":"h100-80g","quota":1}}
	}`)
	if got := put("train-new", newBody); got != http.StatusConflict {
		t.Fatalf("new workload under deleting pool status=%d, want 409", got)
	}
}
