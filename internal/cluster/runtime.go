package cluster

import (
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/provider/kueue"
)

type Runtime struct {
	Kueue *kueue.Provider
}

func NewRuntime(clients *Clients) (*Runtime, error) {
	if clients == nil || clients.Core == nil || clients.Dynamic == nil {
		return nil, fmt.Errorf("cluster clients are required")
	}
	kubeClient := kueue.NewKubeClient(clients.Core, clients.Dynamic)
	return &Runtime{
		Kueue: kueue.NewProvider(kubeClient),
	}, nil
}
