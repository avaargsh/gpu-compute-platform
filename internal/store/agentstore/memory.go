package agentstore

import (
	"bytes"
	"context"
	"encoding/json"
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
	return m.upsertDesiredLocked(clusterID, in)
}

func (m *Memory) CreateWorkloadDesired(
	_ context.Context,
	clusterID domain.ID,
	poolID domain.ID,
	acceleratorClass string,
	in agent.DesiredResource,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for candidateClusterID, items := range m.desired {
		for _, existing := range items {
			if existing.Kind != "Workload" || existing.ID != in.ID {
				continue
			}
			if candidateClusterID != clusterID {
				return ErrIdentityConflict
			}
			if existing.DeletionTimestamp != nil {
				return ErrDesiredNotDeleting
			}
			if existing.Generation != in.Generation {
				return ErrStaleGeneration
			}
			equal, err := desiredSpecEqual(existing.Spec, in.Spec)
			if err != nil {
				return err
			}
			if !equal {
				return ErrStaleGeneration
			}
			return nil
		}
	}

	var pool *agent.DesiredResource
	items := m.desired[clusterID]
	for i := range items {
		if items[i].Kind == "ComputePool" && items[i].ID == poolID {
			pool = &items[i]
			break
		}
	}
	if pool == nil {
		return ErrComputePoolNotFound
	}
	if pool.DeletionTimestamp != nil {
		return ErrComputePoolDeleting
	}
	bound, err := acceleratorBindingExists(
		pool.Spec,
		acceleratorClass,
	)
	if err != nil {
		return err
	}
	if !bound {
		return ErrAcceleratorBindingNotFound
	}
	return m.upsertDesiredLocked(clusterID, in)
}

func (m *Memory) upsertDesiredLocked(
	clusterID domain.ID,
	in agent.DesiredResource,
) error {
	key := string(clusterID) + "/" + in.Kind + "/" + string(in.ID)
	consumeTombstone := false
	if tombstoneGeneration, ok := m.tombstones[key]; ok {
		if in.Generation <= tombstoneGeneration {
			return ErrStaleGeneration
		}
		consumeTombstone = true
	}
	items := m.desired[clusterID]
	for i := range items {
		if items[i].Kind == in.Kind && items[i].ID == in.ID {
			if in.Generation < items[i].Generation {
				return ErrStaleGeneration
			}
			if in.Generation == items[i].Generation {
				equal, err := desiredSpecEqual(items[i].Spec, in.Spec)
				if err != nil {
					return err
				}
				if !equal {
					return ErrStaleGeneration
				}
			}
			if items[i].DeletionTimestamp != nil {
				in.DeletionTimestamp = items[i].DeletionTimestamp
				in.Finalizers = append([]string(nil), items[i].Finalizers...)
			}
			items[i] = in
			m.desired[clusterID] = items
			if consumeTombstone {
				delete(m.tombstones, key)
			}
			return nil
		}
	}
	m.desired[clusterID] = append(items, in)
	if consumeTombstone {
		delete(m.tombstones, key)
	}
	return nil
}

func (m *Memory) MarkDesiredDeleting(_ context.Context, clusterID domain.ID, kind string, resourceID domain.ID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *Memory) FinalizeDesired(
	_ context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	generation int64,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finalizeDesiredLocked(
		clusterID,
		kind,
		resourceID,
		generation,
		"",
		false,
	)
}

func (m *Memory) FinalizeDesiredOwned(
	_ context.Context,
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	generation int64,
	owner string,
) error {
	if owner == "" {
		return ErrLeaseLost
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finalizeDesiredLocked(
		clusterID,
		kind,
		resourceID,
		generation,
		owner,
		true,
	)
}

func (m *Memory) finalizeDesiredLocked(
	clusterID domain.ID,
	kind string,
	resourceID domain.ID,
	generation int64,
	owner string,
	requireLease bool,
) error {
	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	if requireLease {
		current, ok := m.leases[key]
		if !ok || current.owner != owner || !current.until.After(m.now().UTC()) {
			return ErrLeaseLost
		}
	}

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

	previous := make(map[string]agent.Observation, len(m.observations[clusterID]))
	for _, item := range m.observations[clusterID] {
		previous[item.Kind+"/"+string(item.ID)] = item
	}
	out := append([]agent.Observation(nil), in...)
	filtered := out[:0]
	for i := range out {
		if out[i].LeaseOwner != "" {
			key := string(clusterID) + "/" + out[i].Kind + "/" + string(out[i].ID)
			current, ok := m.leases[key]
			if !ok || current.owner != out[i].LeaseOwner || !current.until.After(m.now().UTC()) {
				return ErrLeaseLost
			}
		}

		currentGeneration := int64(0)
		for _, desired := range m.desired[clusterID] {
			if desired.Kind == out[i].Kind && desired.ID == out[i].ID {
				currentGeneration = desired.Generation
				break
			}
		}
		if currentGeneration == 0 {
			// Desired state is the management-plane source of truth. Never
			// persist an observation for an identity/generation that has no
			// current desired resource; otherwise a future recreate could
			// inherit an observation that predates its intent.
			continue
		}
		if out[i].ObservedGeneration != currentGeneration {
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

	foundDesired := false
	for _, item := range m.desired[clusterID] {
		if item.Kind == kind && item.ID == resourceID {
			foundDesired = true
			break
		}
	}
	if !foundDesired {
		return false, nil
	}

	key := string(clusterID) + "/" + kind + "/" + string(resourceID)
	now := m.now().UTC()
	current, ok := m.leases[key]
	if ok && current.owner != owner && current.until.After(now) {
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

func containsFinalizer(finalizers []string, target string) bool {
	for _, finalizer := range finalizers {
		if finalizer == target {
			return true
		}
	}
	return false
}

func desiredSpecEqual(left, right map[string]any) (bool, error) {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return false, fmt.Errorf("marshal existing desired spec: %w", err)
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		return false, fmt.Errorf("marshal incoming desired spec: %w", err)
	}
	return bytes.Equal(leftJSON, rightJSON), nil
}


func acceleratorBindingExists(
	spec map[string]any,
	acceleratorClass string,
) (bool, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return false, fmt.Errorf("marshal compute pool desired spec: %w", err)
	}
	var projected struct {
		AcceleratorBindings []domain.AcceleratorBinding `json:"acceleratorBindings"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		return false, fmt.Errorf("decode compute pool desired spec: %w", err)
	}
	for _, binding := range projected.AcceleratorBindings {
		if binding.Class == acceleratorClass {
			return true, nil
		}
	}
	return false, nil
}
