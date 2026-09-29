package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPlacementMigrationCrashReplayGoldenPath(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	bindings.pools["pool-1"] = Placement{ClusterID: "cluster-a", Provider: "kueue"}
	bindings.migrations["pool-1"] = map[domain.ID]domain.PlacementMigration{
		"migration-1": {
			Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
			PoolID: "pool-1", SourceClusterID: "cluster-a", SourceGeneration: 7,
			TargetClusterID: "cluster-b", Phase: domain.PlacementMigrationProjecting,
		},
	}
	resources := agentstore.NewMemory()
	for _, cluster := range []domain.ID{"cluster-a", "cluster-b"} {
		generation := int64(7)
		if cluster == "cluster-b" {
			generation = 3
		}
		if err := resources.UpsertDesired(ctx, cluster, agent.DesiredResource{
			Kind: "ComputePool", ID: "pool-1", Generation: generation, Spec: map[string]any{},
		}); err != nil {
			t.Fatal(err)
		}
		if err := resources.Report(ctx, cluster, []agent.Observation{{
			Kind: "ComputePool", ID: "pool-1", ObservedGeneration: generation,
			Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
			EvidenceRefs: []string{"k8s://" + string(cluster) + "/pool-1"},
		}}); err != nil {
			t.Fatal(err)
		}
	}

	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()
	post := func(path string, want int) {
		t.Helper()
		resp, err := server.Client().Post(server.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s status=%d, want %d", path, resp.StatusCode, want)
		}
	}
	base := "/api/v1/compute-pools/pool-1/migrations/migration-1"

	post(base+"/prepare-cutover", http.StatusOK)
	post(base+"/prepare-cutover", http.StatusOK)
	migration, err := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.TargetGeneration != 3 {
		t.Fatalf("target generation=%d, want 3", migration.TargetGeneration)
	}

	post(base+"/cutover", http.StatusOK)
	post(base+"/cutover", http.StatusOK)
	placement, err := bindings.ResolvePool(ctx, "pool-1")
	if err != nil || placement.ClusterID != "cluster-b" {
		t.Fatalf("placement=%#v err=%v", placement, err)
	}

	post(base+"/verify-target", http.StatusOK)
	post(base+"/verify-target", http.StatusOK)
	post(base+"/retire-source", http.StatusAccepted)
	post(base+"/retire-source", http.StatusAccepted)

	source, found, err := resources.GetDesired(ctx, "cluster-a", "ComputePool", "pool-1")
	if err != nil || !found || source.DeletionTimestamp == nil {
		t.Fatalf("source deletion not durable: found=%v desired=%#v err=%v", found, source, err)
	}
	if err := resources.FinalizeDesired(ctx, "cluster-a", "ComputePool", "pool-1", 7); err != nil {
		t.Fatal(err)
	}

	post(base+"/retire-source", http.StatusOK)
	post(base+"/retire-source", http.StatusOK)
	migration, err = bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != domain.PlacementMigrationSucceeded {
		t.Fatalf("phase=%s, want Succeeded", migration.Phase)
	}
	if _, found, err := resources.GetDesired(ctx, "cluster-a", "ComputePool", "pool-1"); err != nil || found {
		t.Fatalf("source survived finalization: found=%v err=%v", found, err)
	}
	if finalized, ok, err := resources.FinalizedGeneration(ctx, "cluster-a", "ComputePool", "pool-1"); err != nil || !ok || finalized != 7 {
		t.Fatalf("finalized generation=%d ok=%v err=%v", finalized, ok, err)
	}
}
