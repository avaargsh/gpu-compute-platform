package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPrepareCutoverRequiresSyncedReadyTargetPool(t *testing.T) {
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
	if _, err := bindings.UpdatePlacementMigration(ctx, "pool-1", "migration-1", domain.PlacementMigrationProjecting, nil, nil); err != nil {
		t.Fatal(err)
	}

	resources := agentstore.NewMemory()
	if err := resources.UpsertDesired(ctx, "cluster-b", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 2, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/prepare-cutover", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("unobserved target status=%d, want 409", resp.StatusCode)
	}

	if err := resources.Report(ctx, "cluster-b", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 2,
		Conditions:   []domain.Condition{{Type: "Ready", Status: "True"}},
		EvidenceRefs: []string{"k8s://cluster-b/pool-1"},
	}}); err != nil {
		t.Fatal(err)
	}

	resp, err = server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/prepare-cutover", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ready target status=%d, want 200", resp.StatusCode)
	}
	migration, err := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != domain.PlacementMigrationReadyToCutover {
		t.Fatalf("phase=%s", migration.Phase)
	}
	if len(migration.EvidenceRefs) != 1 || migration.EvidenceRefs[0] != "k8s://cluster-b/pool-1" {
		t.Fatalf("evidence=%#v", migration.EvidenceRefs)
	}
	placement, err := bindings.ResolvePool(ctx, "pool-1")
	if err != nil || placement.ClusterID != "cluster-a" {
		t.Fatalf("prepare-cutover must not mutate binding: placement=%#v err=%v", placement, err)
	}
}

func TestPrepareCutoverRejectsStaleTargetObservation(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	_ = bindings.UpsertClusterBinding(ctx, domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID:   "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	})
	_, _, _ = bindings.CreatePlacementMigration(ctx, domain.PlacementMigration{
		Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
		PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
		Phase: domain.PlacementMigrationRequested,
	})
	_, _ = bindings.UpdatePlacementMigration(ctx, "pool-1", "migration-1", domain.PlacementMigrationProjecting, nil, nil)

	resources := agentstore.NewMemory()
	_ = resources.UpsertDesired(ctx, "cluster-b", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 2, Spec: map[string]any{},
	})
	_ = resources.Report(ctx, "cluster-b", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 1,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}})
	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/prepare-cutover", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale observation status=%d, want 409", resp.StatusCode)
	}
}

type generationDriftStore struct {
	agentstore.Store
	base      *agentstore.Memory
	mutated   bool
	clusterID domain.ID
	poolID    domain.ID
}

func (s *generationDriftStore) GetObservation(
	ctx context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
) (agent.Observation, bool, error) {
	observation, found, err := s.Store.GetObservation(ctx, clusterID, kind, resourceID)
	if err != nil || !found || s.mutated {
		return observation, found, err
	}
	s.mutated = true
	if err := s.base.UpsertDesired(ctx, s.clusterID, agent.DesiredResource{
		Kind: "ComputePool", ID: s.poolID, Generation: observation.ObservedGeneration + 1, Spec: map[string]any{},
	}); err != nil {
		return agent.Observation{}, false, err
	}
	return observation, found, nil
}

func TestPrepareCutoverRejectsGenerationDriftDuringReadWindow(t *testing.T) {
	ctx := t.Context()
	bindings := NewMemoryPlacementResolver()
	_ = bindings.UpsertClusterBinding(ctx, domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID:   "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	})
	_, _, _ = bindings.CreatePlacementMigration(ctx, domain.PlacementMigration{
		Metadata: domain.Metadata{ID: "migration-1", Generation: 1},
		PoolID:   "pool-1", SourceClusterID: "cluster-a", TargetClusterID: "cluster-b",
		Phase: domain.PlacementMigrationRequested,
	})
	_, _ = bindings.UpdatePlacementMigration(ctx, "pool-1", "migration-1", domain.PlacementMigrationProjecting, nil, nil)

	base := agentstore.NewMemory()
	if err := base.UpsertDesired(ctx, "cluster-b", agent.DesiredResource{
		Kind: "ComputePool", ID: "pool-1", Generation: 2, Spec: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := base.Report(ctx, "cluster-b", []agent.Observation{{
		Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 2,
		Conditions: []domain.Condition{{Type: "Ready", Status: "True"}},
	}}); err != nil {
		t.Fatal(err)
	}
	resources := &generationDriftStore{
		Store: base, base: base, clusterID: "cluster-b", poolID: "pool-1",
	}

	server := httptest.NewServer(NewRouterWithDependencies(resources, bindings))
	defer server.Close()
	resp, err := server.Client().Post(
		server.URL+"/api/v1/compute-pools/pool-1/migrations/migration-1/prepare-cutover",
		"application/json",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 when target generation drifts during prepare", resp.StatusCode)
	}
	migration, err := bindings.GetPlacementMigration(ctx, "pool-1", "migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != domain.PlacementMigrationProjecting {
		t.Fatalf("phase=%s, want Projecting", migration.Phase)
	}
	if migration.TargetGeneration != 0 {
		t.Fatalf("target generation=%d, want unset after drift", migration.TargetGeneration)
	}
}
