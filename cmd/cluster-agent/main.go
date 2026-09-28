package main

import (
	"context"
	"log"

	"github.com/avaargsh/gpu-compute-platform/internal/cluster"
)

func main() {
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

	log.Printf(
		"cluster-agent ready: kueue_provider=%t kueue=%t accelerators=%v",
		runtime.Kueue != nil,
		capabilities.Kueue,
		capabilities.Accelerators,
	)
}
