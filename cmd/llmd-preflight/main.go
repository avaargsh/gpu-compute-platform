package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/cluster"
	"github.com/avaargsh/gpu-compute-platform/internal/integration/llmd"
)

func main() {
	os.Exit(run())
}

func run() int {
	namespace := flag.String("namespace", "", "namespace of an existing GAIE v1 InferencePool")
	name := flag.String("pool", "", "name of the existing GAIE v1 InferencePool")
	timeout := flag.Duration("timeout", 10*time.Second, "read-only Kubernetes request timeout")
	flag.Parse()

	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		return 2
	}
	config, err := cluster.RESTConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kubernetes config:", err)
		return 2
	}
	clients, err := cluster.NewClients(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kubernetes clients:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, probeErr := llmd.Probe(
		ctx, clients.Core.Discovery(), clients.Dynamic, *namespace, *name,
	)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "write result:", err)
		return 2
	}
	if probeErr != nil {
		fmt.Fprintln(os.Stderr, "preflight:", probeErr)
		return 2
	}
	if !result.ObjectObserved {
		return 1
	}
	return 0
}
