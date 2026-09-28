package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type Runtime interface {
	ReconcilePool(context.Context, provider.PoolProjection) (provider.PoolObservation, error)
	ReconcileWorkload(context.Context, provider.WorkloadProjection) (provider.WorkloadObservation, error)
}

type Runner struct {
	clusterID domain.ID
	control   ControlPlane
	runtime   Runtime
}

func NewRunner(clusterID domain.ID, control ControlPlane, runtime Runtime) *Runner {
	return &Runner{clusterID: clusterID, control: control, runtime: runtime}
}

func (r *Runner) Sync(ctx context.Context) error {
	if r.clusterID == "" || r.control == nil || r.runtime == nil {
		return fmt.Errorf("cluster id, control plane and runtime are required")
	}

	desired, err := r.control.PullDesired(ctx, r.clusterID)
	if err != nil {
		return fmt.Errorf("pull desired resources: %w", err)
	}

	observations := make([]Observation, 0, len(desired))
	for _, item := range desired {
		observation, err := r.reconcile(ctx, item)
		if err != nil {
			observation = Observation{
				Kind:               item.Kind,
				ID:                 item.ID,
				ObservedGeneration: item.Generation,
				Conditions: []domain.Condition{
					{Type: "Ready", Status: "False", Reason: "ReconcileFailed", Message: err.Error()},
				},
			}
		}
		observations = append(observations, observation)
	}

	if len(observations) == 0 {
		return nil
	}
	if err := r.control.Report(ctx, r.clusterID, observations); err != nil {
		return fmt.Errorf("report observations: %w", err)
	}
	return nil
}

func (r *Runner) reconcile(ctx context.Context, item DesiredResource) (Observation, error) {
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
