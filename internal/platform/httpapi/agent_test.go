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
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
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

func TestClusterStatusReturnsRegisteredCapabilities(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(NewRouterWithAgentStore(store))
	defer server.Close()

	registration := agent.Registration{
		ClusterID:         "cluster-capable",
		AgentVersion:      "v0.2.0",
		KubernetesVersion: "v1.34.1",
		Capabilities: domain.ClusterCapabilities{
			Kueue:           true,
			Schedulers:      []domain.SchedulerCapability{{Name: "kueue", Version: "v0.19.6"}},
			DRAAPIAvailable: true,
			DRAAPIVersion:   "resource.k8s.io/v1",
			Accelerators:    []string{"h100-80g", "metax-c500"},
		},
	}
	body, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Post(
		server.URL+"/api/v1/agent/register",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("register status=%d, want 204", resp.StatusCode)
	}

	heartbeatAt := time.Date(2026, 10, 2, 6, 0, 0, 0, time.UTC)
	heartbeatBody, _ := json.Marshal(agent.Heartbeat{
		ClusterID: registration.ClusterID,
		At:        heartbeatAt,
	})
	heartbeatResp, err := server.Client().Post(
		server.URL+"/api/v1/agent/heartbeat",
		"application/json",
		bytes.NewReader(heartbeatBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	heartbeatResp.Body.Close()
	if heartbeatResp.StatusCode != http.StatusNoContent {
		t.Fatalf("heartbeat status=%d, want 204", heartbeatResp.StatusCode)
	}

	statusResp, err := server.Client().Get(
		server.URL + "/api/v1/clusters/cluster-capable/status",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("status endpoint=%d, want 200", statusResp.StatusCode)
	}
	var status agent.AgentStatus
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Capabilities.Kueue ||
		len(status.Capabilities.Schedulers) != 1 ||
		status.Capabilities.Schedulers[0].Name != "kueue" ||
		status.Capabilities.Schedulers[0].Version != "v0.19.6" ||
		!status.Capabilities.DRAAPIAvailable ||
		status.Capabilities.DRAAPIVersion != "resource.k8s.io/v1" ||
		len(status.Capabilities.Accelerators) != 2 ||
		status.Capabilities.Accelerators[1] != "metax-c500" {
		t.Fatalf("unexpected capabilities: %#v", status.Capabilities)
	}
	if status.LastHeartbeatAt == nil || !status.LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("heartbeat=%v, want %v", status.LastHeartbeatAt, heartbeatAt)
	}
}

func TestClusterStatusReturnsNotFoundBeforeRegistration(t *testing.T) {
	server := httptest.NewServer(NewRouterWithAgentStore(agentstore.NewMemory()))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/api/v1/clusters/unknown/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}
