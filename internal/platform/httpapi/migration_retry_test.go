package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestCutoverRetryReturnsSuccessAfterLostResponse(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-a", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationReadyToCutover,
		},
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()
	url := server.URL + "/api/v1/compute-pools/pool-1/migrations/migration-1/cutover"

	for i := 0; i < 2; i++ {
		resp, err := server.Client().Post(url, "application/json", nil)
		if err != nil {\n\t\t\tt.Fatal(err)\n\t\t}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d status=%d, want 200", i+1, resp.StatusCode)
		}
	}
	placement, err := bindings.ResolvePool(ctx, "pool-1")
	if err != nil || placement.ClusterID != "cluster-b" {
		t.Fatalf("placement=%#v err=%v", placement, err)
	}
}

func TestRetireRetryReturnsSucceededMigration(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationSucceeded,
		},
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/retire-source", "application/json", nil)
	if err != nil {\n\t\tt.Fatal(err)\n\t}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}
