package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Lifecycle struct {
	clusterID         domain.ID
	control           ControlPlane
	runner            *Runner
	agentVersion      string
	kubernetesVersion string
	capabilities      domain.ClusterCapabilities
	interval          time.Duration
	now               func() time.Time
}

func NewLifecycle(
	clusterID domain.ID,
	control ControlPlane,
	runner *Runner,
	agentVersion string,
	kubernetesVersion string,
	capabilities domain.ClusterCapabilities,
	interval time.Duration,
) *Lifecycle {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Lifecycle{
		clusterID:         clusterID,
		control:           control,
		runner:            runner,
		agentVersion:      agentVersion,
		kubernetesVersion: kubernetesVersion,
		capabilities:      capabilities,
		interval:          interval,
		now:               time.Now,
	}
}

func (l *Lifecycle) Run(ctx context.Context) error {
	if l.clusterID == "" || l.control == nil || l.runner == nil {
		return fmt.Errorf("cluster id, control plane and runner are required")
	}
	if err := l.control.Register(ctx, Registration{
		ClusterID:         l.clusterID,
		AgentVersion:      l.agentVersion,
		KubernetesVersion: l.kubernetesVersion,
		Capabilities:      l.capabilities,
	}); err != nil {
		return fmt.Errorf("register agent: %w", err)
	}

	// Reconciliation is a durable control loop: transient heartbeat or sync
	// failures must not terminate the cluster agent. The next tick retries.
	_ = l.tick(ctx)

	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = l.tick(ctx)
		}
	}
}

func (l *Lifecycle) tick(ctx context.Context) error {
	if err := l.control.Heartbeat(ctx, Heartbeat{
		ClusterID: l.clusterID,
		At:        l.now().UTC(),
	}); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if err := l.runner.Sync(ctx); err != nil {
		return fmt.Errorf("sync desired state: %w", err)
	}
	return nil
}
