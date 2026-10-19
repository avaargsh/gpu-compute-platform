package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
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

func (a *AgentAPI) Status(w http.ResponseWriter, r *http.Request) {
	clusterID := domain.ID(r.PathValue("clusterID"))
	if clusterID == "" {
		http.Error(w, "clusterId is required", http.StatusBadRequest)
		return
	}
	status, found, err := a.store.GetAgentStatus(r.Context(), clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, status)
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
	for _, observation := range in.Observations {
		if observation.Kind == "" || observation.ID == "" || observation.LeaseOwner == "" {
			http.Error(w, "observation kind, id and leaseOwner are required", http.StatusBadRequest)
			return
		}
	}
	if err := a.store.Report(r.Context(), in.ClusterID, in.Observations); err != nil {
		if err == agentstore.ErrLeaseLost {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
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

func (a *AgentAPI) FinalizeDesired(w http.ResponseWriter, r *http.Request) {
	var in agent.FinalizeDesiredRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ClusterID == "" || in.Kind == "" || in.ResourceID == "" || in.Generation <= 0 || in.Owner == "" {
		http.Error(w, "clusterId, kind, resourceId, owner and positive generation are required", http.StatusBadRequest)
		return
	}
	if err := a.store.FinalizeDesiredOwned(r.Context(), in.ClusterID, in.Kind, in.ResourceID, in.Generation, in.Owner); err != nil {
		switch err {
		case agentstore.ErrDesiredNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case agentstore.ErrStaleGeneration, agentstore.ErrDesiredNotDeleting, agentstore.ErrLeaseLost:
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublishJobCreateUID is a candidate-only provider identity sink. It can be
// reached ONLY after AgentMTLSRouter verifies SPIFFE cluster certificate and
// stamps an unforgeable, handler-private scope marker. A bearer, browser,
// localhost bypass or synthetic TLS header cannot publish candidate UIDs.
// The store rechecks original Management audit and live lease in PostgreSQL.
func (a *AgentAPI) PublishJobCreateUID(w http.ResponseWriter, r *http.Request) {
	cluster, authenticated := r.Context().Value(verifiedAgentScopeKey{}).(string)
	if !authenticated || cluster == "" {
		http.Error(w, "authenticated cluster agent required", http.StatusUnauthorized)
		return
	}
	var in struct {
		ClusterID          string `json:"clusterId"`
		OperationID        string `json:"operationId"`
		ProjectID          string `json:"projectId"`
		WorkloadID         string `json:"workloadId"`
		Generation         int64  `json:"generation"`
		Namespace          string `json:"namespace"`
		JobName            string `json:"jobName"`
		JobUID             string `json:"jobUid"`
		JobResourceVersion string `json:"jobResourceVersion"`
		LeaseOwner         string `json:"leaseOwner"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ClusterID != cluster {
		http.Error(w, "cluster agent scope mismatch", http.StatusForbidden)
		return
	}
	persist, ok := a.store.(interface {
		RecordJobCreateUID(context.Context, verifiedops.JobCreateUIDBinding) error
	})
	if !ok {
		http.Error(w, "UID persistence unavailable", http.StatusServiceUnavailable)
		return
	}
	err := persist.RecordJobCreateUID(r.Context(), verifiedops.JobCreateUIDBinding{
		OperationID: in.OperationID, ClusterID: domain.ID(in.ClusterID),
		ProjectID: in.ProjectID, WorkloadID: domain.ID(in.WorkloadID),
		Generation: in.Generation, Namespace: in.Namespace, JobName: in.JobName,
		JobUID: in.JobUID, JobResourceVersion: in.JobResourceVersion,
		LeaseOwner: in.LeaseOwner,
	})
	if err != nil {
		switch {
		case errors.Is(err, verifiedops.ErrCreateUIDUnproven),
			errors.Is(err, verifiedops.ErrCreateUIDConflict),
			errors.Is(err, agentstore.ErrLeaseLost):
			http.Error(w, "UID binding rejected", http.StatusConflict)
		default:
			http.Error(w, "UID persistence unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// PrepareJobCreateAttempt requires the private scoped identity from mTLS.
// It commits the one-shot attempt prior to the external Job CREATE.
func (a *AgentAPI) PrepareJobCreateAttempt(w http.ResponseWriter, r *http.Request) {
	cluster, authenticated := r.Context().Value(verifiedAgentScopeKey{}).(string)
	if !authenticated || cluster == "" {
		http.Error(w, "authenticated cluster agent required", http.StatusUnauthorized)
		return
	}
	var in verifiedops.JobCreateAttempt
	if !decodeJSON(w, r, &in) { return }
	if string(in.ClusterID) != cluster {
		http.Error(w, "cluster agent scope mismatch", http.StatusForbidden)
		return
	}
	persist, ok := a.store.(interface {
		PrepareJobCreateAttempt(context.Context, verifiedops.JobCreateAttempt) error
	})
	if !ok {
		http.Error(w, "durable CREATE attempt unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := persist.PrepareJobCreateAttempt(r.Context(), in); err != nil {
		switch {
		case errors.Is(err, verifiedops.ErrCreateUIDUnproven),
			errors.Is(err, verifiedops.ErrCreateAttemptUnresolved),
			errors.Is(err, agentstore.ErrLeaseLost):
			http.Error(w, "CREATE attempt denied or unresolved", http.StatusConflict)
		default:
			http.Error(w, "durable CREATE attempt unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
