package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func TestPostgresAgentRegistrationPersistsCapabilitiesAndHeartbeat(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-capability-registration")
	capabilities := domain.ClusterCapabilities{
		Kueue:           true,
		Schedulers:      []domain.SchedulerCapability{{Name: "kueue", Version: "v0.19.6"}},
		DRAAPIAvailable: true,
		DRAAPIVersion:   "resource.k8s.io/v1",
		Accelerators:    []string{"h100-80g", "metax-c500"},
	}

	if err := store.Register(ctx, agent.Registration{
		ClusterID:         clusterID,
		AgentVersion:      "v0.2.0",
		KubernetesVersion: "v1.34.1",
		Capabilities:      capabilities,
	}); err != nil {
		t.Fatal(err)
	}
	heartbeatAt := time.Date(2026, 10, 2, 6, 0, 0, 0, time.UTC)
	if err := store.Heartbeat(ctx, agent.Heartbeat{
		ClusterID: clusterID,
		At:        heartbeatAt,
	}); err != nil {
		t.Fatal(err)
	}

	restarted := New(db)
	status, found, err := restarted.GetAgentStatus(ctx, clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("registered cluster status not found after store reconstruction")
	}
	if status.AgentVersion != "v0.2.0" || status.KubernetesVersion != "v1.34.1" {
		t.Fatalf("unexpected versions: %#v", status.Registration)
	}
	if !status.Capabilities.Kueue ||
		len(status.Capabilities.Schedulers) != 1 ||
		status.Capabilities.Schedulers[0].Version != "v0.19.6" ||
		!status.Capabilities.DRAAPIAvailable ||
		status.Capabilities.DRAAPIVersion != "resource.k8s.io/v1" ||
		len(status.Capabilities.Accelerators) != 2 ||
		status.Capabilities.Accelerators[0] != "h100-80g" ||
		status.Capabilities.Accelerators[1] != "metax-c500" {
		t.Fatalf("unexpected capabilities: %#v", status.Capabilities)
	}
	if status.LastHeartbeatAt == nil || !status.LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("heartbeat=%v, want %v", status.LastHeartbeatAt, heartbeatAt)
	}
}
