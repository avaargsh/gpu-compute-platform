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
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
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
	return NewRunnerWithLeaseOwner(clusterID, control, runtime, "")
}

func NewRunnerWithLeaseOwner(
	clusterID domain.ID,
	control ControlPlane,
	runtime Runtime,
	leaseOwner string,
) *Runner {
	if leaseOwner == "" {
		leaseOwner = newLeaseOwner()
	}
	return &Runner{
		clusterID:  clusterID,
		control:    control,
		runtime:    runtime,
		now:        time.Now,
		retries:    make(map[string]retryState),
		leaseOwner: leaseOwner,
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
		claimed, err := r.control.ClaimReconcileLease(ctx, lease)
		if err != nil {
			return fmt.Errorf("claim reconcile lease for %s: %w", key, err)
		}
		if !claimed {
			continue
		}

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

			delete(r.retries, key)
			finalObservation := Observation{
				Kind:               item.Kind,
				ID:                 item.ID,
				ObservedGeneration: item.Generation,
				LeaseOwner:         r.leaseOwner,
				Conditions: []domain.Condition{{
					Type: "Ready", Status: "False", Reason: "Deleted",
					Message: "provider resources are gone",
				}},
				EvidenceRefs: deletion.EvidenceRefs,
			}
			if err := r.control.Report(ctx, r.clusterID, []Observation{finalObservation}); err != nil {
				_ = r.control.ReleaseReconcileLease(ctx, lease)
				return fmt.Errorf("report final observation for %s: %w", key, err)
			}
			if err := r.control.FinalizeDesired(ctx, FinalizeDesiredRequest{
				ClusterID: r.clusterID, Kind: item.Kind, ResourceID: item.ID,
				Generation: item.Generation, Owner: r.leaseOwner,
			}); err != nil {
				_ = r.control.ReleaseReconcileLease(ctx, lease)
				return fmt.Errorf("finalize desired resource %s: %w", key, err)
			}
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

		observation.LeaseOwner = r.leaseOwner
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
		projection.OperationID = item.OperationID
		poolBindings, ok := bindings[projection.PoolID]
		if !ok {
			return Observation{}, fmt.Errorf("accelerator bindings not found for pool %s", projection.PoolID)
		}
		binding, ok := poolBindings[projection.Accelerator.Class]
		if !ok {
			return Observation{}, fmt.Errorf("accelerator binding not found: %s", projection.Accelerator.Class)
		}
		projection.AcceleratorBinding = binding
		if projection.OperationID != "" {
			resolver, available := r.control.(CreateAttemptResolver)
			if !available {
				return Observation{}, fmt.Errorf("durable CREATE attempt resolver unavailable")
			}
			resolution, resolveErr := resolver.ResolveJobCreateAttempt(
				ctx, projection.OperationID, r.clusterID, projection.WorkloadID, projection.Generation,
			)
			if resolveErr != nil {
				return Observation{}, provider.MarkRetryable(fmt.Errorf("resolve durable CREATE attempt: %w", resolveErr))
			}
			switch resolution.State {
			case verifiedops.JobCreateAttemptNotFound:
				// A new provider CREATE is allowed only when the server proves no
				// durable PREPARED attempt exists. Both callbacks are mandatory.
				publisher, canPublish := r.control.(CreateUIDPublisher)
				preparer, canPrepare := r.control.(CreateAttemptPreparer)
				if !canPublish || !canPrepare {
					return Observation{}, fmt.Errorf("durable CREATE prepare/publish transport unavailable")
				}
				projection.OnBeforeCreate = func(callCtx context.Context, intent provider.JobCreateIntent) error {
					return preparer.PrepareJobCreateAttempt(callCtx, verifiedops.JobCreateAttempt{
						OperationID: projection.OperationID, ClusterID: r.clusterID,
						ProjectID: string(projection.ProjectID), WorkloadID: projection.WorkloadID,
						Generation: projection.Generation, Namespace: intent.Namespace,
						JobName: intent.Name, LeaseOwner: r.leaseOwner,
					})
				}
				projection.OnCreateACK = func(callCtx context.Context, ack provider.JobCreateACK) error {
					return publisher.PublishJobCreateUID(callCtx, JobCreateUIDCandidate{
						ClusterID: r.clusterID, OperationID: projection.OperationID,
						ProjectID: string(projection.ProjectID), WorkloadID: projection.WorkloadID,
						Generation: projection.Generation, Namespace: ack.Namespace,
						JobName: ack.Name, JobUID: ack.UID,
						JobResourceVersion: ack.ResourceVersion, LeaseOwner: r.leaseOwner,
					})
				}
			case verifiedops.JobCreateAttemptUnknown:
				// The provider may have accepted CREATE before the prior process died.
				// Never repeat CREATE or promote a name/label match to ownership.
				return Observation{
					Kind: item.Kind, ID: item.ID, ObservedGeneration: item.Generation,
					Conditions: []domain.Condition{{
						Type: "Ready", Status: "False", Reason: "CreateAttemptUnresolved",
						Message: "prior provider CREATE outcome is unknown; automatic CREATE retry is blocked",
					}},
				}, nil
			case verifiedops.JobCreateAttemptStale:
				return Observation{
					Kind: item.Kind, ID: item.ID, ObservedGeneration: item.Generation,
					Conditions: []domain.Condition{{
						Type: "Ready", Status: "False", Reason: "CreateAttemptStale",
						Message: "persisted provider CREATE attempt no longer matches current Desired state",
					}},
				}, nil
			case verifiedops.JobCreateAttemptACKBound:
				b := resolution.Binding
				if b == nil || b.OperationID != projection.OperationID || b.ClusterID != r.clusterID ||
					b.ProjectID != string(projection.ProjectID) || b.WorkloadID != projection.WorkloadID ||
					b.Generation != projection.Generation || b.Namespace != projection.Namespace ||
					b.JobUID == "" || b.JobResourceVersion == "" || b.JobName == "" {
					return Observation{
						Kind: item.Kind, ID: item.ID, ObservedGeneration: item.Generation,
						Conditions: []domain.Condition{{
							Type: "Ready", Status: "False", Reason: "CreateAttemptStale",
							Message: "persisted CREATE ACK binding does not match current Workload identity",
						}},
					}, nil
				}
				// A bound retry is read-only: the provider must verify the exact
				// live UID and must not call CREATE or mutate the ResourceClaim.
				projection.ExpectedCreateUID = b.JobUID
			default:
				return Observation{}, fmt.Errorf("unsupported durable CREATE attempt state %q", resolution.State)
			}
		}

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
		projection.OperationID = item.OperationID
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
			if binding.Class == "" {
				return nil, fmt.Errorf("accelerator binding class is required in pool %s", item.ID)
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
