package agentstore

import (
	"context"
	"sync"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type Memory struct {
	mu            sync.RWMutex
	registrations map[domain.ID]agent.Registration
	heartbeats    map[domain.ID]agent.Heartbeat
	desired       map[domain.ID][]agent.DesiredResource
	observations  map[domain.ID][]agent.Observation
}

func NewMemory() *Memory {
	return &Memory{
		registrations: make(map[domain.ID]agent.Registration),
		heartbeats:    make(map[domain.ID]agent.Heartbeat),
		desired:       make(map[domain.ID][]agent.DesiredResource),
		observations:  make(map[domain.ID][]agent.Observation),
	}
}

func (m *Memory) Register(_ context.Context, in agent.Registration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registrations[in.ClusterID] = in
	return nil
}

func (m *Memory) Heartbeat(_ context.Context, in agent.Heartbeat) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeats[in.ClusterID] = in
	return nil
}

func (m *Memory) Desired(_ context.Context, clusterID domain.ID) ([]agent.DesiredResource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]agent.DesiredResource(nil), m.desired[clusterID]...), nil
}


func (m *Memory) UpsertDesired(_ context.Context, clusterID domain.ID, in agent.DesiredResource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.desired[clusterID]
	for i := range items {
		if items[i].Kind == in.Kind && items[i].ID == in.ID {
			items[i] = in
			m.desired[clusterID] = items
			return nil
		}
	}
	m.desired[clusterID] = append(items, in)
	return nil
}

func (m *Memory) DeleteDesired(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.desired[clusterID]
	out := items[:0]
	for _, item := range items {
		if item.Kind != kind || item.ID != resourceID {
			out = append(out, item)
		}
	}
	m.desired[clusterID] = append([]agent.DesiredResource(nil), out...)
	return nil
}

func (m *Memory) Report(_ context.Context, clusterID domain.ID, in []agent.Observation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observations[clusterID] = append([]agent.Observation(nil), in...)
	return nil
}

func (m *Memory) SetDesired(clusterID domain.ID, in []agent.DesiredResource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.desired[clusterID] = append([]agent.DesiredResource(nil), in...)
}

func (m *Memory) Observations(clusterID domain.ID) []agent.Observation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]agent.Observation(nil), m.observations[clusterID]...)
}
