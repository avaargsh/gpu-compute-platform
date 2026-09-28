package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type AgentAPI struct {
	store agentstore.Store
}

func NewAgentAPI(store agentstore.Store) *AgentAPI {
	return &AgentAPI{store: store}
}

func (a *AgentAPI) Register(w http.ResponseWriter, r *http.Request) {
	var in agent.Registration
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ClusterID == "" {
		http.Error(w, "clusterId is required", http.StatusBadRequest)
		return
	}
	if err := a.store.Register(r.Context(), in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *AgentAPI) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var in agent.Heartbeat
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ClusterID == "" {
		http.Error(w, "clusterId is required", http.StatusBadRequest)
		return
	}
	if err := a.store.Heartbeat(r.Context(), in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *AgentAPI) Desired(w http.ResponseWriter, r *http.Request) {
	clusterID := domain.ID(r.URL.Query().Get("clusterId"))
	if clusterID == "" {
		http.Error(w, "clusterId is required", http.StatusBadRequest)
		return
	}
	items, err := a.store.Desired(r.Context(), clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *AgentAPI) Report(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClusterID    domain.ID           `json:"clusterId"`
		Observations []agent.Observation `json:"observations"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ClusterID == "" {
		http.Error(w, "clusterId is required", http.StatusBadRequest)
		return
	}
	if err := a.store.Report(r.Context(), in.ClusterID, in.Observations); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, in any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(in)
}

func (a *AgentAPI) ClaimReconcileLease(w http.ResponseWriter, r *http.Request) {
	var in agent.ReconcileLeaseRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	claimed, err := a.store.ClaimReconcileLease(r.Context(), in.ClusterID, in.Kind, in.ResourceID, in.Owner, in.TTLSeconds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, agent.ReconcileLeaseResponse{Claimed: claimed})
}

func (a *AgentAPI) ReleaseReconcileLease(w http.ResponseWriter, r *http.Request) {
	var in agent.ReconcileLeaseRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := a.store.ReleaseReconcileLease(r.Context(), in.ClusterID, in.Kind, in.ResourceID, in.Owner); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
