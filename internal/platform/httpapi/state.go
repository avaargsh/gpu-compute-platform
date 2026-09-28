package httpapi

import (
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type StateAPI struct {
	store agentstore.Store
}

type resourceState struct {
	Kind               string             `json:"kind"`
	ID                 domain.ID          `json:"id"`
	DesiredGeneration  int64              `json:"desiredGeneration"`
	ObservedGeneration int64              `json:"observedGeneration"`
	SyncState          string             `json:"syncState"`
	Conditions         []domain.Condition `json:"conditions,omitempty"`
	EvidenceRefs       []string           `json:"evidenceRefs,omitempty"`
}

func NewStateAPI(store agentstore.Store) *StateAPI {
	return &StateAPI{store: store}
}

func (a *StateAPI) Get(w http.ResponseWriter, r *http.Request) {
	clusterID := domain.ID(r.PathValue("clusterID"))
	kind := r.PathValue("kind")
	resourceID := domain.ID(r.PathValue("resourceID"))
	if clusterID == "" || kind == "" || resourceID == "" {
		http.Error(w, "clusterID, kind and resourceID are required", http.StatusBadRequest)
		return
	}

	desired, found, err := a.store.GetDesired(r.Context(), clusterID, kind, resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	observation, observed, err := a.store.GetObservation(r.Context(), clusterID, kind, resourceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := resourceState{
		Kind: desired.Kind, ID: desired.ID, DesiredGeneration: desired.Generation,
		SyncState: "Reconciling",
	}
	if observed {
		out.ObservedGeneration = observation.ObservedGeneration
		out.Conditions = observation.Conditions
		out.EvidenceRefs = observation.EvidenceRefs
		switch {
		case observation.ObservedGeneration == desired.Generation:
			out.SyncState = "Synced"
		case observation.ObservedGeneration > desired.Generation:
			out.SyncState = "Inconsistent"
		}
	}
	writeJSON(w, http.StatusOK, out)
}
