package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestAgentDesiredAndReportRoundTrip(t *testing.T) {
	store := agentstore.NewMemory()
	store.SetDesired("cluster-a", []agent.DesiredResource{
		{Kind: "ComputePool", ID: "pool-1", Generation: 3},
	})
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
	lease, err := store.ClaimReconcileLease(context.Background(), "cluster-a", "ComputePool", "pool-1", "agent-test", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !lease.Claimed {
		t.Fatal("expected reconcile lease to be claimed")
	}

	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-a",
		"observations": []agent.Observation{
			{
				Kind:               "ComputePool",
				ID:                 "pool-1",
				ObservedGeneration: 3,
				LeaseOwner:         lease.Owner,
				LeaseEpoch:         lease.Epoch,
			},
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
	if reportResp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected report status: %d", reportResp.StatusCode)
	}

	observed := store.Observations("cluster-a")
	if len(observed) != 1 || observed[0].ObservedGeneration != 3 {
		t.Fatalf("unexpected observations: %#v", observed)
	}

}
