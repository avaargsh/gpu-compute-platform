package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestMemoryPlacementRejectsCrossClusterRebind(t *testing.T) {
	store := NewMemoryPlacementResolver()
	if err := store.UpsertClusterBinding(context.Background(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1}, PoolID: "pool-1", ClusterID: "cluster-a", Provider: "kueue",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClusterBinding(context.Background(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 2}, PoolID: "pool-1", ClusterID: "cluster-a", Provider: "volcano",
	}); err != nil {
		t.Fatalf("same-cluster update: %v", err)
	}
	err := store.UpsertClusterBinding(context.Background(), domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 3}, PoolID: "pool-1", ClusterID: "cluster-b", Provider: "volcano",
	})
	if !errors.Is(err, agentstore.ErrPlacementMigrationRequired) {
		t.Fatalf("error=%v, want placement migration required", err)
	}
}

func TestBindingAPIRejectsCrossClusterRebind(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	server := httptest.NewServer(NewRouterWithDependencies(agentstore.NewMemory(), bindings))
	defer server.Close()

	put := func(body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/compute-pools/pool-1/binding", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	first := put(`{"metadata":{"generation":1},"poolId":"pool-1","clusterId":"cluster-a","provider":"kueue"}`)
	first.Body.Close()
	if first.StatusCode != http.StatusNoContent {
		t.Fatalf("first status=%d", first.StatusCode)
	}
	moved := put(`{"metadata":{"generation":2},"poolId":"pool-1","clusterId":"cluster-b","provider":"kueue"}`)
	defer moved.Body.Close()
	if moved.StatusCode != http.StatusConflict {
		t.Fatalf("move status=%d, want 409", moved.StatusCode)
	}
}

func TestMemoryPlacementRejectsProjectCrossClusterRebind(t *testing.T) {
	resolver := NewMemoryPlacementResolver()
	ctx := context.Background()

	if err := resolver.UpsertProjectBinding(ctx, domain.ProjectBinding{
		Metadata: domain.Metadata{Generation: 1},
		ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "team-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := resolver.UpsertProjectBinding(ctx, domain.ProjectBinding{
		Metadata: domain.Metadata{Generation: 2},
		ProjectID: "project-1", ClusterID: "cluster-a", Namespace: "team-b",
	}); err != nil {
		t.Fatalf("same-cluster namespace update failed: %v", err)
	}
	err := resolver.UpsertProjectBinding(ctx, domain.ProjectBinding{
		Metadata: domain.Metadata{Generation: 3},
		ProjectID: "project-1", ClusterID: "cluster-b", Namespace: "team-b",
	})
	if !errors.Is(err, agentstore.ErrPlacementMigrationRequired) {
		t.Fatalf("expected placement migration requirement, got %v", err)
	}
}

func TestProjectBindingAPIRejectsCrossClusterRebind(t *testing.T) {
	bindings := NewMemoryPlacementResolver()
	router := NewRouterWithDependencies(agentstore.NewMemory(), bindings)

	first := httptest.NewRequest(http.MethodPut, "/api/v1/projects/project-1/binding", strings.NewReader(`{"metadata":{"generation":1},"projectId":"project-1","clusterId":"cluster-a","namespace":"team-a"}`))
	first.Header.Set("Content-Type", "application/json")
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusNoContent {
		t.Fatalf("initial project binding status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}

	move := httptest.NewRequest(http.MethodPut, "/api/v1/projects/project-1/binding", strings.NewReader(`{"metadata":{"generation":2},"projectId":"project-1","clusterId":"cluster-b","namespace":"team-a"}`))
	move.Header.Set("Content-Type", "application/json")
	moveResponse := httptest.NewRecorder()
	router.ServeHTTP(moveResponse, move)
	if moveResponse.Code != http.StatusConflict {
		t.Fatalf("cross-cluster project binding status=%d body=%s", moveResponse.Code, moveResponse.Body.String())
	}
}
