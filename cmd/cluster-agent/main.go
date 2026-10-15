package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
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
	endpoint, err := url.Parse(controlPlaneURL)
	if err != nil || endpoint.User != nil || endpoint.Hostname() == "" ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" {
		log.Fatal("CONTROL_PLANE_URL must be an HTTP(S) origin without credentials, path or query")
	}
	caFile := os.Getenv("CLUSTER_AGENT_TLS_CA")
	certFile := os.Getenv("CLUSTER_AGENT_TLS_CERT")
	keyFile := os.Getenv("CLUSTER_AGENT_TLS_KEY")
	secure := caFile != "" || certFile != "" || keyFile != ""
	var transport *http.Client
	if secure {
		if endpoint.Scheme != "https" {
			log.Fatal("Agent client certificate authentication requires CONTROL_PLANE_URL=https")
		}
		transport, err = httpclient.NewClusterMTLSClient(caFile, certFile, keyFile, string(clusterID))
		if err != nil {
			log.Fatalf("Agent mTLS client configuration: %v", err)
		}
	} else {
		// Keep existing localhost Golden Path compatibility, never send
		// unauthenticated Agent traffic to remote HTTP endpoints.
		if endpoint.Scheme != "http" || (endpoint.Hostname() != "localhost" &&
			endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "::1") {
			log.Fatal("remote Cluster Agents require TLS client certificate authentication")
		}
		transport = &http.Client{Timeout: 10 * time.Second}
	}

	config, err := cluster.RESTConfig()
	if err != nil {
		log.Fatalf("kubernetes config: %v", err)
	}
	clients, err := cluster.NewClients(config)
	if err != nil {
		log.Fatalf("kubernetes clients: %v", err)
	}
	serverVersion, err := clients.Core.Discovery().ServerVersion()
	if err != nil {
		log.Fatalf("discover kubernetes version: %v", err)
	}

	runtime, err := cluster.NewRuntime(clients)
	if err != nil {
		log.Fatalf("cluster runtime: %v", err)
	}

	capabilities, err := clients.Discover(context.Background())
	if err != nil {
		log.Fatalf("discover cluster capabilities: %v", err)
	}
	capabilities.Providers = runtime.ProviderNames()
	log.Printf(
		"cluster capabilities: providers=%v kueue=%t schedulers=%v dra=%t draApiVersion=%q accelerators=%v",
		capabilities.Providers,
		capabilities.Kueue,
		capabilities.Schedulers,
		capabilities.DRAAPIAvailable,
		capabilities.DRAAPIVersion,
		capabilities.Accelerators,
	)

	control := httpclient.New(controlPlaneURL, transport)

	agentInstanceID := os.Getenv("AGENT_INSTANCE_ID")
	runner := agent.NewRunnerWithLeaseOwner(
		clusterID,
		control,
		runtime,
		agentInstanceID,
	)

	syncInterval := 15 * time.Second
	if raw := os.Getenv("AGENT_SYNC_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			log.Fatalf("invalid AGENT_SYNC_INTERVAL %q", raw)
		}
		syncInterval = parsed
	}
	log.Printf(
		"cluster agent process identity=%q syncInterval=%s",
		agentInstanceID,
		syncInterval,
	)
	lifecycle := agent.NewLifecycle(
		clusterID,
		control,
		runner,
		agentVersion,
		serverVersion.GitVersion,
		capabilities,
		syncInterval,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := lifecycle.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("cluster agent stopped: %v", err)
	}
	log.Print("cluster-agent stopped")
}
