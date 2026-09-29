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
	epoch int64
	until time.Time
}

type Memory struct {
	mu            sync.RWMutex
	registrations map[domain.ID]agent.Registration
	heartbeats    map[domain.ID]agent.Heartbeat
	desired       map[domain.ID][]agent.DesiredResource
	observations  map[domain.ID][]agent.Observation
	leases        map[string]memoryLease
	tombstones    map[string]int64
	now           func() time.Time
}

func NewMemory() *Memory {
	return &Memory{
		registrations: make(map[domain.ID]agent.Registration),
		heartbeats:    make(map[domain.ID]agent.Heartbeat),
		desired:       make(map[domain.ID][]agent.DesiredResource),
		observations:  make(map[domain.ID][]agent.Observation),
		leases:        make(map[string]memoryLease),
		tombstones:    make(map[string]int64),
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
	key := string(clusterID) + "/" + in.Kind + "/" + string(in.ID)
	if tombstoneGeneration, ok := m.tombstones[key]; ok {
		if in.Generation <= tombstoneGeneration {
			return ErrStaleGeneration
		}
		delete(m.tombstones, key)
	}
	items := m.desired[clusterID]
	for i := range items {
		if items[i].Kind == in.Kind && items[i].ID == in.ID {
			if in.Generation < items[i].Generation {
				return ErrStaleGeneration
			}
			if items[i].DeletionTimestamp != nil {
				in.DeletionTimestamp = items[i].DeletionTimestamp
				in.Finalizers = append([]string(nil), items[i].Finalizers...)
			}
			items[i] = in
			m.desired[clusterID] = items
			return nil
		}
	}
	m.desired[clusterID] = append(items, in)
	return nil
}

func (m *Memory) MarkDesiredDeleting(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	if len(leaseToken) == 2 {
		leaseOwner, _ := leaseToken[0].(string)
		leaseEpoch, _ := leaseToken[1].(int64)
		lease, ok := m.leases[key]
		if !ok || lease.owner != leaseOwner || lease.epoch != leaseEpoch || !lease.until.After(m.now().UTC()) {
			return ErrStaleReconcileLease
		}
	}
	items := m.desired[clusterID]
	for i := range items {
		if items[i].Kind == kind && items[i].ID == resourceID {
			if items[i].DeletionTimestamp == nil {
				ts := at.UTC()
				items[i].DeletionTimestamp = &ts
			}
			hasFinalizer := false
			for _, finalizer := range items[i].Finalizers {
				if finalizer == ProviderCleanupFinalizer {
					hasFinalizer = true
					break
				}
			}
			if !hasFinalizer {
				items[i].Finalizers = append(items[i].Finalizers, ProviderCleanupFinalizer)
			}
			m.desired[clusterID] = items
			return nil
		}
	}
	return ErrDesiredNotFound
}

func (m *Memory) FinalizeDesired(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID, generation int64, leaseToken ...any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.desired[clusterID]
	out := items[:0]
	found := false
	for _, item := range items {
		if item.Kind == kind && item.ID == resourceID {
			if item.Generation != generation {
				return ErrStaleGeneration
			}
			if item.DeletionTimestamp == nil || !containsFinalizer(item.Finalizers, ProviderCleanupFinalizer) {
				return ErrDesiredNotDeleting
			}
			found = true
			continue
		}
		out = append(out, item)
	}
	if !found {
		return ErrDesiredNotFound
	}
	m.desired[clusterID] = append([]agent.DesiredResource(nil), out...)

	m.tombstones[key] = generation
	delete(m.leases, key)

	observations := m.observations[clusterID]
	filtered := observations[:0]
	for _, item := range observations {
		if item.Kind == kind && item.ID == resourceID {
			continue
		}
		filtered = append(filtered, item)
	}
	m.observations[clusterID] = append([]agent.Observation(nil), filtered...)
	return nil
}

func (m *Memory) FinalizedGeneration(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID) (int64, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	generation, ok := m.tombstones[string(clusterID)+"/"+kind+"/"+string(resourceID)]
	return generation, ok, nil
}

func (m *Memory) Report(_ context.Context, clusterID domain.ID, in []agent.Observation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range in {
		if item.LeaseOwner == "" && item.LeaseEpoch == 0 {
			continue
		}
		key := string(clusterID) + "/" + item.Kind + "/" + string(item.ID)
		lease, ok := m.leases[key]
		if !ok || lease.owner != item.LeaseOwner || lease.epoch != item.LeaseEpoch || !lease.until.After(m.now().UTC()) {
			return ErrStaleReconcileLease
		}
	}

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
		if currentGeneration == 0 {
			key := string(clusterID) + "/" + out[i].Kind + "/" + string(out[i].ID)
			if tombstoneGeneration, ok := m.tombstones[key]; ok && out[i].ObservedGeneration <= tombstoneGeneration {
				continue
			}
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
) (agent.ReconcileLeaseGrant, error) {
	if clusterID == "" || kind == "" || resourceID == "" || owner == "" {
		return agent.ReconcileLeaseGrant{}, fmt.Errorf("reconcile lease identity is required")
	}
	if ttlSeconds <= 0 {
		return agent.ReconcileLeaseGrant{}, fmt.Errorf("reconcile lease ttl must be positive")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	now := m.now().UTC()
	current, ok := m.leases[key]
	if ok && current.owner != owner && current.until.After(now) {
		return agent.ReconcileLeaseGrant{}, nil
	}
	epoch := int64(1)
	if ok {
		epoch = current.epoch
		if current.owner != owner {
			epoch++
		}
	}
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
	m.leases[key] = memoryLease{owner: owner, epoch: epoch, until: expiresAt}
	return agent.ReconcileLeaseGrant{Claimed: true, Owner: owner, Epoch: epoch, ExpiresAt: expiresAt}, nil
}

func (m *Memory) ReleaseReconcileLease(
	_ context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	owner string,
	epoch ...int64,
) error {
	if owner == "" {
		return fmt.Errorf("reconcile lease owner is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	if current, ok := m.leases[key]; ok && current.owner == owner && (len(epoch) == 0 || current.epoch == epoch[0]) {
		delete(m.leases, key)
	}
	return nil
}

func containsFinalizer(finalizers []string, target string) bool {
	for _, finalizer := range finalizers {
		if finalizer == target {
			return true
		}
	}
	return false
}
