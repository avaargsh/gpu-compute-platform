package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type BindingAPI struct {
	store BindingStore
}

func NewBindingAPI(store BindingStore) *BindingAPI {
	return &BindingAPI{store: store}
}

func (a *BindingAPI) UpsertProject(w http.ResponseWriter, r *http.Request) {
	projectID := domain.ID(r.PathValue("projectID"))
	var in domain.ProjectBinding
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid project binding", http.StatusBadRequest)
		return
	}
	if projectID == "" || in.ProjectID == "" || in.ClusterID == "" || in.Namespace == "" || in.Metadata.Generation <= 0 {
		http.Error(w, "projectId, clusterId, namespace and positive generation are required", http.StatusBadRequest)
		return
	}
	if in.ProjectID != projectID {
		http.Error(w, "project id must match path", http.StatusBadRequest)
		return
	}
	if err := a.store.UpsertProjectBinding(r.Context(), in); err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) || errors.Is(err, agentstore.ErrPlacementMigrationRequired) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *BindingAPI) UpsertPool(w http.ResponseWriter, r *http.Request) {
	poolID := domain.ID(r.PathValue("poolID"))
	var in domain.ClusterBinding
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid cluster binding", http.StatusBadRequest)
		return
	}
	if poolID == "" || in.PoolID == "" || in.ClusterID == "" || in.Provider == "" || in.Metadata.Generation <= 0 {
		http.Error(w, "poolId, clusterId, provider and positive generation are required", http.StatusBadRequest)
		return
	}
	if in.PoolID != poolID {
		http.Error(w, "pool id must match path", http.StatusBadRequest)
		return
	}
	if err := a.store.UpsertClusterBinding(r.Context(), in); err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) ||
			errors.Is(err, agentstore.ErrPlacementMigrationRequired) ||
			errors.Is(err, agentstore.ErrProviderBindingImmutable) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
