package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestStateAPIProjectsGenerationDrift(t *testing.T) {
	store := agentstore.NewMemory()
	ctx := context.Background()
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "train-1", Generation: 8, Spec: map[string]any{"image": "v8"}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()

	get := func() resourceState {
		resp, err := server.Client().Get(server.URL + "/api/v1/internal/clusters/cluster-a/state/Workload/train-1")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		var out resourceState
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := get(); got.SyncState != "Reconciling" || got.DesiredGeneration != 8 || got.ObservedGeneration != 0 {
		t.Fatalf("unobserved state=%#v", got)
	}
	if err := store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: "train-1", ObservedGeneration: 7}}); err != nil {
		t.Fatal(err)
	}
	if got := get(); got.SyncState != "Reconciling" || got.ObservedGeneration != 7 {
		t.Fatalf("drift state=%#v", got)
	}
	if err := store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: "train-1", ObservedGeneration: 8, EvidenceRefs: []string{"reconciled"}}}); err != nil {
		t.Fatal(err)
	}
	if got := get(); got.SyncState != "Synced" || got.EvidenceRefs[0] != "reconciled" {
		t.Fatalf("synced state=%#v", got)
	}
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "train-1", Generation: 7, Spec: map[string]any{"image": "v7"}}); err == nil {
		t.Fatal("expected stale desired write to fail")
	}
}
