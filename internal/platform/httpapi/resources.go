package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type ResourceAPI struct {
	store agentstore.Store
}

type placedComputePool struct {
	ClusterID domain.ID          `json:"clusterId"`
	Namespace string             `json:"namespace"`
	Resource  domain.ComputePool `json:"resource"`
}

type placedWorkload struct {
	ClusterID domain.ID        `json:"clusterId"`
	Namespace string           `json:"namespace"`
	Resource  domain.Workload  `json:"resource"`
}

func NewResourceAPI(store agentstore.Store) *ResourceAPI {
	return &ResourceAPI{store: store}
}

func (a *ResourceAPI) UpsertComputePool(w http.ResponseWriter, r *http.Request) {
	var in placedComputePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid compute pool", http.StatusBadRequest)
		return
	}
	if in.ClusterID == "" || in.Namespace == "" || in.Resource.Metadata.ID == "" || in.Resource.Metadata.Generation <= 0 || in.Resource.ProjectID == "" {
		http.Error(w, "clusterId, namespace, resource id, projectId and positive generation are required", http.StatusBadRequest)
		return
	}
	spec := map[string]any{
		"projectID":    in.Resource.ProjectID,
		"namespace":    in.Namespace,
		"accelerators": in.Resource.Spec.Accelerators,
		"scheduling":   in.Resource.Spec.Scheduling,
	}
	if err := a.store.UpsertDesired(r.Context(), in.ClusterID, agent.DesiredResource{
		Kind: "ComputePool", ID: in.Resource.Metadata.ID, Generation: in.Resource.Metadata.Generation, Spec: spec,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ResourceAPI) UpsertWorkload(w http.ResponseWriter, r *http.Request) {
	var in placedWorkload
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid workload", http.StatusBadRequest)
		return
	}
	if in.ClusterID == "" || in.Namespace == "" || in.Resource.Metadata.ID == "" || in.Resource.Metadata.Generation <= 0 || in.Resource.ProjectID == "" || in.Resource.PoolID == "" {
		http.Error(w, "clusterId, namespace, resource id, projectId, poolId and positive generation are required", http.StatusBadRequest)
		return
	}
	spec := map[string]any{
		"projectID":   in.Resource.ProjectID,
		"poolID":      in.Resource.PoolID,
		"namespace":   in.Namespace,
		"image":       in.Resource.Spec.Image,
		"command":     in.Resource.Spec.Command,
		"accelerator": in.Resource.Spec.Accelerator,
	}
	if err := a.store.UpsertDesired(r.Context(), in.ClusterID, agent.DesiredResource{
		Kind: "Workload", ID: in.Resource.Metadata.ID, Generation: in.Resource.Metadata.Generation, Spec: spec,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
