package management

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Execution is a bounded, independently stored Agent Observation projection.
// It does NOT report Kubernetes live state, authenticated Job success,
// checkpoint durability, or resource deletion completion.
type executionCondition struct {
	Type string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

func decodeExecutionConditions(raw []byte) ([]executionCondition, bool) {
	var values []executionCondition
	if len(raw) == 0 || raw[0] != '[' || len(raw) > 32768 ||
		json.Unmarshal(raw, &values) != nil || len(values) > 48 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(values))
	for _, c := range values {
		if c.Type == "" || len(c.Type) > 128 ||
			(c.Status != "True" && c.Status != "False" && c.Status != "Unknown") ||
			len(c.Reason) > 256 || len(c.Message) > 1024 {
			return nil, false
		}
		if _, duplicate := seen[c.Type]; duplicate { return nil, false }
		seen[c.Type] = struct{}{}
	}
	return values, true
}

func (s *Service) execution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cluster, project, resource := r.PathValue("clusterID"), r.PathValue("projectID"), r.PathValue("resourceID")
	if !safeID.MatchString(cluster) || !safeID.MatchString(project) || !safeID.MatchString(resource) {
		problem(w, http.StatusBadRequest)
		return
	}
	token := bearerToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		problem(w, http.StatusUnauthorized)
		return
	}
	id, err := s.Auth.Verify(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrIntrospectionUnavailable) {
			problem(w, http.StatusServiceUnavailable)
		} else {
			w.Header().Set("WWW-Authenticate", "Bearer")
			problem(w, http.StatusUnauthorized)
		}
		return
	}
	var gen int64
	var deleting bool
	var observed sql.NullInt64
	var raw []byte
	var observedAt sql.NullTime
	// Security-definer function performs the current project + binding +
	// Desired project identity join in a SINGLE statement. Runtime login
	// deliberately has NO SELECT on the Observation table.
	err = s.DB.QueryRowContext(r.Context(), `SELECT desired_generation,deleting,
       observed_generation,conditions,observed_at
FROM public.management_scoped_workload_observation($1,$2,$3,$4,$5)`,
		id.Issuer, id.Subject, cluster, project, resource,
	).Scan(&gen, &deleting, &observed, &raw, &observedAt)
	if err == sql.ErrNoRows {
		problem(w, http.StatusNotFound)
		return
	}
	if err != nil || gen < 1 || (observed.Valid && (observed.Int64 < 0 || !observedAt.Valid)) {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	conditions := []executionCondition{}
	var observationGeneration any
	var observationTime any
	if observed.Valid {
		var ok bool
		conditions, ok = decodeExecutionConditions(raw)
		if !ok {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		observationGeneration = observed.Int64
		observationTime = observedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"clusterId": cluster, "projectId": project, "kind": "Workload", "id": resource,
		"desiredGeneration": gen, "deleting": deleting,
		"observedGeneration": observationGeneration, "observedAt": observationTime,
		"conditions": conditions,
	})
}
