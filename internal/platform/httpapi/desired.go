package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type DesiredAPI struct {
	store agentstore.Store
}

func NewDesiredAPI(store agentstore.Store) *DesiredAPI {
	return &DesiredAPI{store: store}
}

func (a *DesiredAPI) Upsert(w http.ResponseWriter, r *http.Request) {
	clusterID := domain.ID(r.PathValue("clusterID"))
	kind := r.PathValue("kind")
	resourceID := domain.ID(r.PathValue("resourceID"))
	if clusterID == "" || kind == "" || resourceID == "" {
		http.Error(w, "clusterID, kind and resourceID are required", http.StatusBadRequest)
		return
	}

	var in agent.DesiredResource
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid desired resource", http.StatusBadRequest)
		return
	}
	in.Kind = kind
	in.ID = resourceID
	if in.Generation <= 0 {
		http.Error(w, "positive generation is required", http.StatusBadRequest)
		return
	}
	if in.Spec == nil {
		http.Error(w, "spec is required", http.StatusBadRequest)
		return
	}
	if err := a.store.UpsertDesired(r.Context(), clusterID, in); err != nil {
		if errors.Is(err, agentstore.ErrStaleGeneration) ||
			errors.Is(err, agentstore.ErrDesiredDeleting) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *DesiredAPI) Delete(w http.ResponseWriter, r *http.Request) {
	clusterID := domain.ID(r.PathValue("clusterID"))
	kind := r.PathValue("kind")
	resourceID := domain.ID(r.PathValue("resourceID"))
	if clusterID == "" || kind == "" || resourceID == "" {
		http.Error(w, "clusterID, kind and resourceID are required", http.StatusBadRequest)
		return
	}
	if err := a.store.MarkDesiredDeleting(r.Context(), clusterID, kind, resourceID, time.Now().UTC()); err != nil {
		if errors.Is(err, agentstore.ErrDesiredNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
