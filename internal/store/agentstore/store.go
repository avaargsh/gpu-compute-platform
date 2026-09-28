package agentstore

import (
	"context"
	"errors"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

var ErrStaleGeneration = errors.New("stale generation")
var ErrIdentityConflict = errors.New("resource identity conflict")

type Store interface {
	Register(context.Context, agent.Registration) error
	Heartbeat(context.Context, agent.Heartbeat) error
	Desired(context.Context, domain.ID) ([]agent.DesiredResource, error)
	GetDesired(context.Context, domain.ID, string, domain.ID) (agent.DesiredResource, bool, error)
	LocateDesired(context.Context, string, domain.ID) (domain.ID, agent.DesiredResource, bool, error)
	GetObservation(context.Context, domain.ID, string, domain.ID) (agent.Observation, bool, error)
	UpsertDesired(context.Context, domain.ID, agent.DesiredResource) error
	DeleteDesired(context.Context, domain.ID, string, domain.ID) error
	Report(context.Context, domain.ID, []agent.Observation) error
}
