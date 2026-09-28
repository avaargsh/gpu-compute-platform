package agentstore

import (
	"context"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Store interface {
	Register(context.Context, agent.Registration) error
	Heartbeat(context.Context, agent.Heartbeat) error
	Desired(context.Context, domain.ID) ([]agent.DesiredResource, error)
	Report(context.Context, domain.ID, []agent.Observation) error
}
