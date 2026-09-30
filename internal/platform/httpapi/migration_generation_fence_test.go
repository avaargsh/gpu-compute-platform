package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestRetireSourceRejectsRecreatedSourceGeneration(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", SourceGeneration: 7,
			TargetClusterID: "cluster-b", Phase: domain.PlacementMigrationRetiring,
		},
	}
	resources := agentstore.NewMemory()
	if err := resources.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 8, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/retire-source", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	desired, found, err := resources.GetDesired(ctx, "cluster-a", "ComputePool", "pool-1")
	if err != nil || !found {
		t.Fatalf("source desired missing: found=%v err=%v", found, err)
	}
	if desired.DeletionTimestamp != nil {
		t.Fatal("generation mismatch must not mark recreated source deleting")
	}
}

func TestRetireSourceRejectsWrongGenerationTombstone(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", SourceGeneration: 7,
			TargetClusterID: "cluster-b", Phase: domain.PlacementMigrationRetiring,
		},
	}
	resources := agentstore.NewMemory()
	if err := resources.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 8, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := resources.MarkDesiredDeleting(ctx, "cluster-a", "ComputePool", "pool-1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	recordGoneForTest(t, resources, "cluster-a", "ComputePool", "pool-1", 8)
	if err := resources.FinalizeDesired(ctx, "cluster-a", "ComputePool", "pool-1", 8); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/retire-source", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	migration, err := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != domain.PlacementMigrationRetiring {
		t.Fatalf("phase=%s, want Retiring", migration.Phase)
	}
}
