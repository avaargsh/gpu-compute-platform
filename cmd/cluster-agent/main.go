package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/agent/httpclient"
	"github.com/avaargsh/gpu-compute-platform/internal/cluster"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

const agentVersion = "dev"

func main() {
	clusterID := domain.ID(os.Getenv("CLUSTER_ID"))
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	if clusterID == "" || controlPlaneURL == "" {
		log.Fatal("CLUSTER_ID and CONTROL_PLANE_URL are required")
	}

	config, err := cluster.RESTConfig()
	if err != nil {
		log.Fatalf("kubernetes config: %v", err)
	}
	clients, err := cluster.NewClients(config)
	if err != nil {
		log.Fatalf("kubernetes clients: %v", err)
	}
	runtime, err := cluster.NewRuntime(clients)
	if err != nil {
		log.Fatalf("cluster runtime: %v", err)
	}

	capabilities, err := clients.Discover(context.Background())
	if err != nil {
		log.Fatalf("discover cluster capabilities: %v", err)
	}
	log.Printf("cluster capabilities: kueue=%t accelerators=%v", capabilities.Kueue, capabilities.Accelerators)

	control := httpclient.New(controlPlaneURL, &http.Client{Timeout: 10 * time.Second})
	runner := agent.NewRunner(clusterID, control, runtime)
	lifecycle := agent.NewLifecycle(clusterID, control, runner, agentVersion, "", 15*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := lifecycle.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("cluster agent stopped: %v", err)
	}
	log.Print("cluster-agent stopped")
}
