package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPlacementMigrationCutoverAtomicallyMovesBinding(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	if err := bindings.UpsertClusterBinding(ctx, domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID:   "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := bindings.CreatePlacementMigration(ctx, domain.PlacementMigration{
		Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
		PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
		Phase: domain.PlacementMigrationRequested,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bindings.UpdatePlacementMigration(ctx, "pool-1", "migration-1", domain.PlacementMigrationProjecting, nil, nil)
	_, _ = bindings.UpdatePlacementMigration(ctx, "pool-1", "migration-1", domain.PlacementMigrationReadyToCutover, nil, []string{"k8s://cluster-b/pool-1"})

	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/cutover", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cutover status=%d, want 200", resp.StatusCode)
	}
	placement, err := bindings.ResolvePool(ctx, "pool-1")
	if err != nil {
		t.Fatal(err)
	}
	if placement.ClusterID != "cluster-b" {
		t.Fatalf("cluster=%s, want cluster-b", placement.ClusterID)
	}
	migration, err := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != domain.PlacementMigrationCutover {
		t.Fatalf("phase=%s, want Cutover", migration.Phase)
	}
}

func TestPlacementMigrationCutoverRejectsSourceDrift(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-x", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationReadyToCutover,
		},
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/cutover", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if bindings.pools["pool-1"].ClusterID != "cluster-x" {
		t.Fatal("failed cutover mutated binding")
	}
	if bindings.migrations["pool-1"]["migration-1"].Phase != domain.PlacementMigrationReadyToCutover {
		t.Fatal("failed cutover mutated migration phase")
	}
}

func TestPlacementMigrationCutoverRequiresReadyPhase(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-a", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationProjecting,
		},
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/cutover", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if bindings.pools["pool-1"].ClusterID != "cluster-a" {
		t.Fatal("premature cutover mutated binding")
	}
}
