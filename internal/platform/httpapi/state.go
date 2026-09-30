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
	ObservationLeaseOwner string           `json:"observationLeaseOwner,omitempty"`
}

func NewStateAPI(store agentstore.Store) *StateAPI {
	return &StateAPI{store: store}
}

func projectResourceState(desiredGeneration int64, observationFound bool, observationGeneration int64, conditions []domain.Condition, evidenceRefs []string) resourceState {
	out := resourceState{DesiredGeneration: desiredGeneration, SyncState: "Reconciling"}
	if observationFound {
		out.ObservedGeneration = observationGeneration
		out.Conditions = conditions
		out.EvidenceRefs = evidenceRefs
		switch {
		case observationGeneration == desiredGeneration:
			out.SyncState = "Synced"
		case observationGeneration > desiredGeneration:
			out.SyncState = "Inconsistent"
		}
	}
	return out
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

	out := projectResourceState(desired.Generation, observed, observation.ObservedGeneration, observation.Conditions, observation.EvidenceRefs)
	out.Kind = desired.Kind
	out.ID = desired.ID
	out.ObservationLeaseOwner = observation.LeaseOwner
	writeJSON(w, http.StatusOK, out)
}
