package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestAgentDesiredAndReportRoundTrip(t *testing.T) {
	store := agentstore.NewMemory()
	store.SetDesired("cluster-a", []agent.DesiredResource{
		{Kind: "ComputePool", ID: "pool-1", Generation: 3},
	})
	claimed, err := store.ClaimReconcileLease(
		context.Background(), "cluster-a", "ComputePool", "pool-1", "agent-a", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("claim lease: claimed=%t err=%v", claimed, err)
	}
	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/v1/agent/desired?clusterId=cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var desired []agent.DesiredResource
	if err := json.NewDecoder(resp.Body).Decode(&desired); err != nil {
		t.Fatal(err)
	}
	if len(desired) != 1 || desired[0].Generation != 3 {
		t.Fatalf("unexpected desired: %#v", desired)
	}

	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-a",
		"observations": []agent.Observation{
			{Kind: "ComputePool", ID: "pool-1", ObservedGeneration: 3, LeaseOwner: "agent-a"},
		},
	})
	reportResp, err := server.Client().Post(
		server.URL+"/api/v1/agent/report",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatal(err)
	}
	reportResp.Body.Close()

	observed := store.Observations("cluster-a")
	if len(observed) != 1 || observed[0].ObservedGeneration != 3 {
		t.Fatalf("unexpected observations: %#v", observed)
	}

}

func TestAgentReportRejectsStaleLeaseOwner(t *testing.T) {
	store := agentstore.NewMemory()
	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind: "Workload", ID: "train-1", Generation: 1,
	}})
	claimed, err := store.ClaimReconcileLease(
		context.Background(), "cluster-a", "Workload", "train-1", "agent-b", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("claim lease: claimed=%t err=%v", claimed, err)
	}

	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()

	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-a",
		"observations": []agent.Observation{{
			Kind:               "Workload",
			ID:                 "train-1",
			ObservedGeneration: 1,
			LeaseOwner:         "agent-a",
		}},
	})
	resp, err := server.Client().Post(
		server.URL+"/api/v1/agent/report",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 409 {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if got := store.Observations("cluster-a"); len(got) != 0 {
		t.Fatalf("stale observation must not persist: %#v", got)
	}
}


func TestAgentFinalizeDesiredTreatsCommittedReplayAsSuccess(t *testing.T) {
	store := agentstore.NewMemory()
	ctx := context.Background()
	store.SetDesired("cluster-a", []agent.DesiredResource{{
		Kind: "Workload", ID: "train-finalize-replay", Generation: 4,
	}})
	if err := store.MarkDesiredDeleting(
		ctx, "cluster-a", "Workload", "train-finalize-replay", time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(
		ctx, "cluster-a", "Workload", "train-finalize-replay", "agent-a", 30,
	)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%t err=%v", claimed, err)
	}

	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()
	payload, _ := json.Marshal(agent.FinalizeDesiredRequest{
		ClusterID:  "cluster-a",
		Kind:       "Workload",
		ResourceID: "train-finalize-replay",
		Generation: 4,
		Owner:      "agent-a",
	})

	for attempt := 1; attempt <= 2; attempt++ {
		resp, err := server.Client().Post(
			server.URL+"/api/v1/agent/finalize-desired",
			"application/json",
			bytes.NewReader(payload),
		)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("attempt %d status=%d, want 204", attempt, resp.StatusCode)
		}
	}
}
