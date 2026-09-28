package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPlacementMigrationStatusRejectsSkippedTransition(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	if err := bindings.UpsertClusterBinding(t.Context(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID:   "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := bindings.CreatePlacementMigration(t.Context(), domain.PlacementMigration{
		Metadata: MetadataForMigration("migration-1"),
		PoolID: "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
		Phase: domain.PlacementMigrationRequested,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/status", bytes.NewBufferString(`{"phase":"Cutover"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	placement, err := bindings.ResolvePool(t.Context(), "pool-1")
	if err != nil || placement.ClusterID != "cluster-a" {
		t.Fatalf("status update must not mutate binding: placement=%#v err=%v", placement, err)
	}
}

func MetadataForMigration(id domain.ID) domain.Metadata {
	return domain.Metadata{ID: id, Generation: 1}
}
