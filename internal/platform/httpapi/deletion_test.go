package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func recordGoneForTest(t *testing.T, store agentstore.Store, clusterID domain.ID, kind string, resourceID domain.ID, generation int64) {
	t.Helper()
	if err := store.Report(context.Background(), clusterID, []agent.Observation{{
		Kind: kind, ID: resourceID, ObservedGeneration: generation,
		Conditions:   []domain.Condition{{Type: "Ready", Status: "False", Reason: "Deleted"}},
		EvidenceRefs: []string{"provider://gone/" + string(resourceID)},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizedStateRetainsEvidenceWithoutRuntimeObservation(t *testing.T) {
	store := agentstore.NewMemory()
	ctx := context.Background()
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "train", Generation: 7}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(ctx, "cluster-a", "Workload", "train", time.Now()); err != nil {
		t.Fatal(err)
	}
	recordGoneForTest(t, store, "cluster-a", "Workload", "train", 7)
	if err := store.FinalizeDesired(ctx, "cluster-a", "Workload", "train", 7); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()
	resp, err := server.Client().Get(server.URL + "/api/v1/internal/clusters/cluster-a/state/Workload/train")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var state resourceState
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state status=%d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.SyncState != "Finalized" || state.DesiredGeneration != 0 || state.ObservedGeneration != 0 || state.DeletionTombstone == nil || state.DeletionTombstone.Generation != 7 || len(state.EvidenceRefs) != 1 {
		t.Fatalf("unexpected final state: %#v", state)
	}
	if _, found, err := store.GetObservation(ctx, "cluster-a", "Workload", "train"); err != nil || found {
		t.Fatalf("final evidence must not recreate runtime observation: found=%t err=%v", found, err)
	}
}
