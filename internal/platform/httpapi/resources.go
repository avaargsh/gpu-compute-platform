package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type ResourceAPI struct {
	store     agentstore.Store
	placement PlacementResolver
}

func NewResourceAPI(store agentstore.Store, placement PlacementResolver) *ResourceAPI {
	return &ResourceAPI{store: store, placement: placement}
}

func (a *ResourceAPI) UpsertComputePool(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	var in domain.ComputePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid compute pool", http.StatusBadRequest)
		return
	}
	if resourceID == "" || in.Metadata.ID == "" || in.Metadata.Generation <= 0 || in.ProjectID == "" {
		http.Error(w, "resource id, projectId and positive generation are required", http.StatusBadRequest)
		return
	}
	if in.Metadata.ID != resourceID {
		http.Error(w, "resource id must match path", http.StatusBadRequest)
		return
	}
	projectPlacement, err := a.placement.ResolveProject(r.Context(), in.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	poolPlacement, err := a.placement.ResolvePool(r.Context(), in.Metadata.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if projectPlacement.ClusterID != poolPlacement.ClusterID {
		http.Error(w, "project and pool bindings target different clusters", http.StatusConflict)
		return
	}
	spec := map[string]any{
		"projectID":    in.ProjectID,
		"namespace":    projectPlacement.Namespace,
		"accelerators": in.Spec.Accelerators,
		"scheduling":   in.Spec.Scheduling,
	}
	if err := a.store.UpsertDesired(r.Context(), poolPlacement.ClusterID, agent.DesiredResource{
		Kind: "ComputePool", ID: in.Metadata.ID, Generation: in.Metadata.Generation, Spec: spec,
	}); err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ResourceAPI) UpsertWorkload(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	var in domain.Workload
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid workload", http.StatusBadRequest)
		return
	}
	if resourceID == "" || in.Metadata.ID == "" || in.Metadata.Generation <= 0 || in.ProjectID == "" || in.PoolID == "" {
		http.Error(w, "resource id, projectId, poolId and positive generation are required", http.StatusBadRequest)
		return
	}
	if in.Metadata.ID != resourceID {
		http.Error(w, "resource id must match path", http.StatusBadRequest)
		return
	}
	projectPlacement, err := a.placement.ResolveProject(r.Context(), in.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	poolPlacement, err := a.placement.ResolvePool(r.Context(), in.PoolID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if projectPlacement.ClusterID != poolPlacement.ClusterID {
		http.Error(w, "project and pool bindings target different clusters", http.StatusConflict)
		return
	}
	spec := map[string]any{
		"projectID":   in.ProjectID,
		"poolID":      in.PoolID,
		"namespace":   projectPlacement.Namespace,
		"image":       in.Spec.Image,
		"command":     in.Spec.Command,
		"accelerator": in.Spec.Accelerator,
	}
	if err := a.store.UpsertDesired(r.Context(), poolPlacement.ClusterID, agent.DesiredResource{
		Kind: "Workload", ID: in.Metadata.ID, Generation: in.Metadata.Generation, Spec: spec,
	}); err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type workloadView struct {
	Spec   map[string]any `json:"spec"`
	Status resourceState  `json:"status"`
}

func (a *ResourceAPI) GetWorkload(w http.ResponseWriter, r *http.Request) {
	resourceID := domain.ID(r.PathValue("resourceID"))
	poolID := domain.ID(r.URL.Query().Get("poolId"))
	if resourceID == "" || poolID == "" {
		http.Error(w, "resource id and poolId are required", http.StatusBadRequest)
		return
	}
	placement, err := a.placement.ResolvePool(r.Context(), poolID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	desired, found, err := a.store.GetDesired(r.Context(), placement.ClusterID, "Workload", resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found || desired.Spec["poolID"] != string(poolID) {
		http.NotFound(w, r)
		return
	}
	observation, observed, err := a.store.GetObservation(r.Context(), placement.ClusterID, "Workload", resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	status := projectResourceState(desired.Generation, observed, observation.ObservedGeneration, observation.Conditions, observation.EvidenceRefs)
	status.Kind = desired.Kind
	status.ID = desired.ID
	writeJSON(w, http.StatusOK, workloadView{Spec: desired.Spec, Status: status})
}
