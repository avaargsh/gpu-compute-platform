package cluster

import (
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Clients struct {
	Core    kubernetes.Interface
	Dynamic dynamic.Interface
}

func NewClients(config *rest.Config) (*Clients, error) {
	if config == nil {
		return nil, fmt.Errorf("kubernetes rest config is required")
	}

	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic kubernetes client: %w", err)
	}
	return &Clients{Core: core, Dynamic: dynamicClient}, nil
}
