package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestRetireSourceUsesDeletionLifecycleBeforeSuccess(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationRetiring,
		},
	}
	resources := agentstore.NewMemory()
	if err := resources.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 7, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := resources.Report(ctx, "cluster-a", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 7,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()
	url := server.URL + "/api/v1/compute-pools/pool-1/migrations/migration-1/retire-source"

	resp, err := server.Client().Post(url, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("first retire status=%d, want 202", resp.StatusCode)
	}
	desired, found, err := resources.GetDesired(ctx, "cluster-a", "ComputePool", "pool-1")
	if err != nil || !found || desired.DeletionTimestamp == nil {
		t.Fatalf("source desired must be deleting: found=%v desired=%#v err=%v", found, desired, err)
	}
	migration, _ := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if migration.Phase != domain.PlacementMigrationRetiring {
		t.Fatalf("phase=%s, want Retiring until provider cleanup finalizes", migration.Phase)
	}

	if err := resources.FinalizeDesired(ctx, "cluster-a", "ComputePool", "pool-1", 7); err != nil {
		t.Fatal(err)
	}
	resp, err = server.Client().Post(url, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final retire status=%d, want 200", resp.StatusCode)
	}
	migration, _ = bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if migration.Phase != domain.PlacementMigrationSucceeded {
		t.Fatalf("phase=%s, want Succeeded", migration.Phase)
	}
	if len(migration.EvidenceRefs) != 1 || migration.EvidenceRefs[0] != "control-plane://placement-migration/source-finalized/generation/7" {
		t.Fatalf("evidence=%#v", migration.EvidenceRefs)
	}
}

func TestRetireSourceRejectsBeforeTargetVerification(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationCutover,
		},
	}
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/retire-source", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
}


func TestRetireSourceWaitsForFinalizationEvidence(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-b", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
			Phase: domain.PlacementMigrationRetiring,
		},
	}
	resources := agentstore.NewMemory()
	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()

	resp, err := server.Client().Post(
		server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/retire-source",
		"application/json",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d, want 202 without finalization tombstone", resp.StatusCode)
	}
	migration, _ := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if migration.Phase != domain.PlacementMigrationRetiring {
		t.Fatalf("phase=%s, want Retiring without finalization evidence", migration.Phase)
	}
	if len(migration.EvidenceRefs) != 0 {
		t.Fatalf("unexpected evidence=%#v", migration.EvidenceRefs)
	}
}
