package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestVerifyTargetRejectsGenerationChangeAfterPrepare(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", SourceGeneration: 7,
			TargetClusterID: "cluster-b", TargetGeneration: 3,
			Phase: domain.PlacementMigrationCutover,
		},
	}
	resources := agentstore.NewMemory()
	if err := resources.UpsertDesired(ctx, "cluster-b", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 4, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := resources.Report(ctx, "cluster-b", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 4,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/verify-target", "application/json", nil)
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
	if migration.Phase != domain.PlacementMigrationCutover {
		t.Fatalf("phase=%s, want Cutover", migration.Phase)
	}
}
