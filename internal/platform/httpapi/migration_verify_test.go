package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestVerifyTargetAdvancesCutoverToRetiring(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationCutover,
			EvidenceRefs: []string{"cutover://pool-1"},
		},
	}
	resources := agentstore.NewMemory()
	if err := resources.UpsertDesired(ctx, "cluster-b", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 3, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := resources.Report(ctx, "cluster-b", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 3,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
		EvidenceRefs: []string{"k8s://cluster-b/pool-1"},
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	migration, err := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != domain.PlacementMigrationRetiring {
		t.Fatalf("phase=%s, want Retiring", migration.Phase)
	}
	if len(migration.EvidenceRefs) != 2 {
		t.Fatalf("evidence=%#v", migration.EvidenceRefs)
	}
}

func TestVerifyTargetRejectsBindingDrift(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-x", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationCutover,
		},
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/verify-target", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if bindings.migrations["pool-1"]["migration-1"].Phase != domain.PlacementMigrationCutover {
		t.Fatal("failed verification advanced migration")
	}
}

func TestVerifyTargetRejectsStaleObservation(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationCutover,
		},
	}
	resources := agentstore.NewMemory()
	_ = resources.UpsertDesired(ctx, "cluster-b", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 4, Spec: map[string]any{},
	})
	_ = resources.Report(ctx, "cluster-b", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 3,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}})
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
	if bindings.migrations["pool-1"]["migration-1"].Phase != domain.PlacementMigrationCutover {
		t.Fatal("stale target advanced migration")
	}
}
