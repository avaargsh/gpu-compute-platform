package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type Runtime interface {
	ReconcilePool(context.Context, provider.PoolProjection) (provider.PoolObservation, error)
	ReconcileWorkload(context.Context, provider.WorkloadProjection) (provider.WorkloadObservation, error)
	DeletePool(context.Context, provider.PoolProjection) (provider.DeletionObservation, error)
	DeleteWorkload(context.Context, provider.WorkloadProjection) (provider.DeletionObservation, error)
}

type retryState struct {
	generation int64
	attempt    int
	nextAt     time.Time
}

type Runner struct {
	clusterID  domain.ID
	control    ControlPlane
	runtime    Runtime
	now        func() time.Time
	retries    map[string]retryState
	leaseOwner string
}

func NewRunner(clusterID domain.ID, control ControlPlane, runtime Runtime) *Runner {
	return &Runner{
		clusterID:  clusterID,
		control:    control,
		runtime:    runtime,
		now:        time.Now,
		retries:    make(map[string]retryState),
		leaseOwner: newLeaseOwner(),
	}
}

func (r *Runner) Sync(ctx context.Context) error {
	if r.clusterID == "" || r.control == nil || r.runtime == nil {
		return fmt.Errorf("cluster id, control plane and runtime are required")
	}

	desired, err := r.control.PullDesired(ctx, r.clusterID)
	if err != nil {
		return fmt.Errorf("pull desired resources: %w", err)
	}
	current := make(map[string]bool, len(desired))
	for _, item := range desired {
		current[item.Kind+"/"+string(item.ID)] = true
	}
	for key := range r.retries {
		if !current[key] {
			delete(r.retries, key)
		}
	}

	bindings, err := indexAcceleratorBindings(desired)
	if err != nil {
		return fmt.Errorf("index accelerator bindings: %w", err)
	}

	for _, item := range desired {
		key := item.Kind + "/" + string(item.ID)
		state, retrying := r.retries[key]
		if retrying && state.generation != item.Generation {
			delete(r.retries, key)
			retrying = false
		}
		if retrying && r.now().Before(state.nextAt) {
			continue
		}

		lease := ReconcileLeaseRequest{
			ClusterID: r.clusterID, Kind: item.Kind, ResourceID: item.ID,
			Owner: r.leaseOwner, TTLSeconds: 120,
		}
		grant, err := r.control.ClaimReconcileLease(ctx, lease)
		if err != nil {
			return fmt.Errorf("claim reconcile lease for %s: %w", key, err)
		}
		if !grant.Claimed {
			continue
		}
		lease.Epoch = grant.Epoch

		if item.DeletionTimestamp != nil {
			if item.Kind == "ComputePool" && hasDependentWorkload(desired, item.ID) {
				if err := r.control.ReleaseReconcileLease(ctx, lease); err != nil {
					return fmt.Errorf("release reconcile lease for %s: %w", key, err)
				}
				continue
			}
			deletion, deleteErr := r.delete(ctx, item, bindings)
			if deleteErr != nil && provider.IsRetryable(deleteErr) {
				r.scheduleRetry(key, item.Generation)
				if err := r.control.ReleaseReconcileLease(ctx, lease); err != nil {
					return fmt.Errorf("release reconcile lease for %s: %w", key, err)
				}
				continue
			}
			if deleteErr != nil {
				if err := r.control.ReleaseReconcileLease(ctx, lease); err != nil {
					return fmt.Errorf("release reconcile lease for %s: %w", key, err)
				}
				return fmt.Errorf("delete provider resource for %s: %w", key, deleteErr)
			}
			if !deletion.Gone {
				r.scheduleRetry(key, item.Generation)
				if err := r.control.ReleaseReconcileLease(ctx, lease); err != nil {
					return fmt.Errorf("release reconcile lease for %s: %w", key, err)
				}
				continue
			}

			finalObservation := Observation{
				LeaseOwner: grant.Owner, LeaseEpoch: grant.Epoch,
				Kind:               item.Kind,
				ID:                 item.ID,
				ObservedGeneration: item.Generation,
				Conditions: []domain.Condition{{
					Type: "Ready", Status: "False", Reason: "Deleted",
					Message:            "provider resources are gone",
					LastTransitionTime: r.now().UTC(),
				}},
				EvidenceRefs: deletion.EvidenceRefs,
			}
			if err := r.control.Report(ctx, r.clusterID, []Observation{finalObservation}); err != nil {
				_ = r.control.ReleaseReconcileLease(ctx, lease)
				return fmt.Errorf("report final observation for %s: %w", key, err)
			}
			if err := r.control.FinalizeDesired(ctx, FinalizeDesiredRequest{
				ClusterID: r.clusterID, Kind: item.Kind, ResourceID: item.ID, Generation: item.Generation,
				LeaseOwner: grant.Owner, LeaseEpoch: grant.Epoch,
			}); err != nil {
				_ = r.control.ReleaseReconcileLease(ctx, lease)
				return fmt.Errorf("finalize desired resource %s: %w", key, err)
			}
			delete(r.retries, key)
			continue
		}

		observation, reconcileErr := r.reconcile(ctx, item, bindings)
		if reconcileErr != nil && provider.IsRetryable(reconcileErr) {
			r.scheduleRetry(key, item.Generation)
			if err := r.control.ReleaseReconcileLease(ctx, lease); err != nil {
				return fmt.Errorf("release reconcile lease for %s: %w", key, err)
			}
			continue
		}

		delete(r.retries, key)
		if reconcileErr != nil {
			observation = Observation{
				Kind:               item.Kind,
				ID:                 item.ID,
				ObservedGeneration: item.Generation,
				Conditions: []domain.Condition{
					{Type: "Ready", Status: "False", Reason: "ReconcileFailed", Message: reconcileErr.Error()},
				},
			}
		}

		observation.LeaseOwner = grant.Owner
		observation.LeaseEpoch = grant.Epoch
		reportErr := r.control.Report(ctx, r.clusterID, []Observation{observation})
		releaseErr := r.control.ReleaseReconcileLease(ctx, lease)
		if reportErr != nil {
			return fmt.Errorf("report observation for %s: %w", key, reportErr)
		}
		if releaseErr != nil {
			return fmt.Errorf("release reconcile lease for %s: %w", key, releaseErr)
		}
	}

	return nil
}

func (r *Runner) reconcile(ctx context.Context, item DesiredResource, bindings map[domain.ID]map[string]domain.AcceleratorBinding) (Observation, error) {
	switch item.Kind {
	case "ComputePool":
		var projection provider.PoolProjection
		if err := decodeSpec(item.Spec, &projection); err != nil {
			return Observation{}, err
		}
		projection.PoolID = item.ID
		projection.ClusterID = r.clusterID
		projection.Generation = item.Generation
		got, err := r.runtime.ReconcilePool(ctx, projection)
		if err != nil {
			return Observation{}, err
		}
		return Observation{
			Kind:               item.Kind,
			ID:                 item.ID,
			ObservedGeneration: got.ObservedGeneration,
			Conditions:         got.Conditions,
			EvidenceRefs:       got.EvidenceRefs,
		}, nil

	case "Workload":
		var projection provider.WorkloadProjection
		if err := decodeSpec(item.Spec, &projection); err != nil {
			return Observation{}, err
		}
		projection.WorkloadID = item.ID
		projection.ClusterID = r.clusterID
		projection.Generation = item.Generation
		poolBindings, ok := bindings[projection.PoolID]
		if !ok {
			return Observation{}, fmt.Errorf("accelerator bindings not found for pool %s", projection.PoolID)
		}
		binding, ok := poolBindings[projection.Accelerator.Class]
		if !ok {
			return Observation{}, fmt.Errorf("accelerator binding not found: %s", projection.Accelerator.Class)
		}
		projection.AcceleratorBinding = binding
		got, err := r.runtime.ReconcileWorkload(ctx, projection)
		if err != nil {
			return Observation{}, err
		}
		return Observation{
			Kind:               item.Kind,
			ID:                 item.ID,
			ObservedGeneration: got.ObservedGeneration,
			Conditions:         got.Conditions,
			EvidenceRefs:       got.EvidenceRefs,
		}, nil

	default:
		return Observation{}, fmt.Errorf("unsupported desired resource kind %q", item.Kind)
	}
}

func (r *Runner) delete(ctx context.Context, item DesiredResource, bindings map[domain.ID]map[string]domain.AcceleratorBinding) (provider.DeletionObservation, error) {
	switch item.Kind {
	case "ComputePool":
		var projection provider.PoolProjection
		if err := decodeSpec(item.Spec, &projection); err != nil {
			return provider.DeletionObservation{}, err
		}
		projection.PoolID = item.ID
		projection.ClusterID = r.clusterID
		projection.Generation = item.Generation
		return r.runtime.DeletePool(ctx, projection)
	case "Workload":
		var projection provider.WorkloadProjection
		if err := decodeSpec(item.Spec, &projection); err != nil {
			return provider.DeletionObservation{}, err
		}
		projection.WorkloadID = item.ID
		projection.ClusterID = r.clusterID
		projection.Generation = item.Generation
		if poolBindings, ok := bindings[projection.PoolID]; ok {
			projection.AcceleratorBinding = poolBindings[projection.Accelerator.Class]
		}
		return r.runtime.DeleteWorkload(ctx, projection)
	default:
		return provider.DeletionObservation{}, fmt.Errorf("unsupported desired resource kind %q", item.Kind)
	}
}

func hasDependentWorkload(desired []DesiredResource, poolID domain.ID) bool {
	for _, item := range desired {
		if item.Kind != "Workload" {
			continue
		}
		var projection provider.WorkloadProjection
		if decodeSpec(item.Spec, &projection) == nil && projection.PoolID == poolID {
			return true
		}
	}
	return false
}

func decodeSpec(spec map[string]any, out any) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode desired spec: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode desired spec: %w", err)
	}
	return nil
}

func indexAcceleratorBindings(desired []DesiredResource) (map[domain.ID]map[string]domain.AcceleratorBinding, error) {
	out := make(map[domain.ID]map[string]domain.AcceleratorBinding)
	for _, item := range desired {
		if item.Kind != "ComputePool" {
			continue
		}
		var projection provider.PoolProjection
		if err := decodeSpec(item.Spec, &projection); err != nil {
			return nil, fmt.Errorf("decode compute pool %s: %w", item.ID, err)
		}
		poolBindings := make(map[string]domain.AcceleratorBinding, len(projection.AcceleratorBindings))
		for _, binding := range projection.AcceleratorBindings {
			if binding.Class == "" || binding.ResourceName == "" || binding.Flavor == "" {
				return nil, fmt.Errorf("accelerator binding is incomplete in pool %s", item.ID)
			}
			if _, exists := poolBindings[binding.Class]; exists {
				return nil, fmt.Errorf("duplicate accelerator binding %s in pool %s", binding.Class, item.ID)
			}
			poolBindings[binding.Class] = binding
		}
		out[item.ID] = poolBindings
	}
	return out, nil
}

func (r *Runner) scheduleRetry(key string, generation int64) {
	state := r.retries[key]
	if state.generation != generation {
		state = retryState{generation: generation}
	}
	state.attempt++
	state.nextAt = r.now().Add(retryBackoff(state.attempt))
	r.retries[key] = state
}

func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Second * time.Duration(1<<(attempt-1))
}

func newLeaseOwner() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}
