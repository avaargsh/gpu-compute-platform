package agentstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

type memoryLease struct {
	owner string
	until time.Time
}

type Memory struct {
	mu            sync.RWMutex
	registrations map[domain.ID]agent.Registration
	heartbeats    map[domain.ID]agent.Heartbeat
	desired       map[domain.ID][]agent.DesiredResource
	observations  map[domain.ID][]agent.Observation
	leases        map[string]memoryLease
	now           func() time.Time
}

func NewMemory() *Memory {
	return &Memory{
		registrations: make(map[domain.ID]agent.Registration),
		heartbeats:    make(map[domain.ID]agent.Heartbeat),
		desired:       make(map[domain.ID][]agent.DesiredResource),
		observations:  make(map[domain.ID][]agent.Observation),
		leases:        make(map[string]memoryLease),
		now:           time.Now,
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

func (m *Memory) GetDesired(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID) (agent.DesiredResource, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.desired[clusterID] {
		if item.Kind == kind && item.ID == resourceID {
			return item, true, nil
		}
	}
	return agent.DesiredResource{}, false, nil
}

func (m *Memory) LocateDesired(_ context.Context, kind string, resourceID domain.ID) (domain.ID, agent.DesiredResource, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var clusterID domain.ID
	var found agent.DesiredResource
	matches := 0
	for candidateClusterID, items := range m.desired {
		for _, item := range items {
			if item.Kind == kind && item.ID == resourceID {
				clusterID = candidateClusterID
				found = item
				matches++
				if matches > 1 {
					return "", agent.DesiredResource{}, false, ErrIdentityConflict
				}
			}
		}
	}
	if matches == 0 {
		return "", agent.DesiredResource{}, false, nil
	}
	return clusterID, found, true, nil
}

func (m *Memory) GetObservation(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID) (agent.Observation, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.observations[clusterID] {
		if item.Kind == kind && item.ID == resourceID {
			return item, true, nil
		}
	}
	return agent.Observation{}, false, nil
}

func (m *Memory) UpsertDesired(_ context.Context, clusterID domain.ID, in agent.DesiredResource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.desired[clusterID]
	for i := range items {
		if items[i].Kind == in.Kind && items[i].ID == in.ID {
			if in.Generation < items[i].Generation {
				return ErrStaleGeneration
			}
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

	previous := make(map[string]agent.Observation, len(m.observations[clusterID]))
	for _, item := range m.observations[clusterID] {
		previous[item.Kind+"/"+string(item.ID)] = item
	}
	out := append([]agent.Observation(nil), in...)
	filtered := out[:0]
	for i := range out {
		currentGeneration := int64(0)
		for _, desired := range m.desired[clusterID] {
			if desired.Kind == out[i].Kind && desired.ID == out[i].ID {
				currentGeneration = desired.Generation
				break
			}
		}
		if currentGeneration != 0 && out[i].ObservedGeneration != currentGeneration {
			continue
		}
		if old, ok := previous[out[i].Kind+"/"+string(out[i].ID)]; ok {
			if out[i].ObservedGeneration < old.ObservedGeneration {
				continue
			}
			out[i].Conditions = MergeConditions(old.Conditions, out[i].Conditions)
		}
		filtered = append(filtered, out[i])
	}
	for key, old := range previous {
		found := false
		for _, item := range filtered {
			if item.Kind+"/"+string(item.ID) == key {
				found = true
				break
			}
		}
		if !found {
			filtered = append(filtered, old)
		}
	}
	m.observations[clusterID] = filtered
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

func (m *Memory) ClaimReconcileLease(
	_ context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	owner string,
	ttlSeconds int64,
) (bool, error) {
	if clusterID == "" || kind == "" || resourceID == "" || owner == "" {
		return false, fmt.Errorf("reconcile lease identity is required")
	}
	if ttlSeconds <= 0 {
		return false, fmt.Errorf("reconcile lease ttl must be positive")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	now := m.now().UTC()
	current, ok := m.leases[key]
	if ok && current.owner != owner && !current.until.Before(now) {
		return false, nil
	}
	m.leases[key] = memoryLease{
		owner: owner,
		until: now.Add(time.Duration(ttlSeconds) * time.Second),
	}
	return true, nil
}

func (m *Memory) ReleaseReconcileLease(
	_ context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	owner string,
) error {
	if owner == "" {
		return fmt.Errorf("reconcile lease owner is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	if current, ok := m.leases[key]; ok && current.owner == owner {
		delete(m.leases, key)
	}
	return nil
}
