package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPlacementMigrationCreateDoesNotMutateBinding(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	if err := bindings.UpsertClusterBinding(t.Context(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID: "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()

	body := []byte(`{"metadata":{"id":"migration-1","generation":1},"poolId":"pool-1","sourceClusterId":"cluster-a","targetClusterId":"cluster-b"}`)
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	var created domain.PlacementMigration
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Phase != domain.PlacementMigrationRequested {
		t.Fatalf("phase=%s", created.Phase)
	}

	placement, err := bindings.ResolvePool(t.Context(), "pool-1")
	if err != nil {
		t.Fatal(err)
	}
	if placement.ClusterID != "cluster-a" {
		t.Fatalf("migration creation mutated binding to %s", placement.ClusterID)
	}

	getResp, err := server.Client().Get(server.URL + "/api/v1/compute-pools/pool-1/migrations/migration-1")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", getResp.StatusCode)
	}
}

func TestPlacementMigrationRequiresCurrentSource(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	if err := bindings.UpsertClusterBinding(t.Context(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID: "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()

	body := []byte(`{"metadata":{"id":"migration-1","generation":1},"poolId":"pool-1","sourceClusterId":"cluster-x","targetClusterId":"cluster-b"}`)
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
}

func TestPlacementMigrationCreateIsIdempotent(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	if err := bindings.UpsertClusterBinding(t.Context(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID: "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()

	body := []byte(`{"metadata":{"id":"migration-1","generation":1},"poolId":"pool-1","sourceClusterId":"cluster-a","targetClusterId":"cluster-b"}`)
	first, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first status=%d", first.StatusCode)
	}

	retry, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Body.Close()
	if retry.StatusCode != http.StatusOK {
		t.Fatalf("retry status=%d, want 200", retry.StatusCode)
	}
}
