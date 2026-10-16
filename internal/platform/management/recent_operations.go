package management

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Recent operations are an authorized, bounded discovery hint, NOT a receipt,
// a replay permit, or evidence that an Agent performed the desired mutation.
// No request body, spec, digest, or other operator's identity is disclosed.
const recentOperationLimit = 25

type recentOperation struct {
	RequestID   string    `json:"requestId"`
	Kind        string    `json:"kind"`
	ResourceID  string    `json:"resourceId"`
	Action      string    `json:"action"`
	CommittedAt time.Time `json:"committedAt"`
}

type recentOperationList struct {
	Items     []recentOperation `json:"items"`
	Truncated bool              `json:"truncated"`
}

// recentOperations retains one trust boundary: the same SQL snapshot must
// prove the live grant and binding for the EXACT introspected issuer+subject.
// The pilot exposes the most recent 25 only; no claim of complete history.
func (s *Service) recentOperations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cluster, project := r.PathValue("clusterID"), r.PathValue("projectID")
	if !safeID.MatchString(cluster) || !safeID.MatchString(project) || r.URL.RawQuery != "" {
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
	// JOIN prevents stale grants and cross-cluster reads. Requiring the
	// original audit subject also denies other operators in the same Project.
	rows, err := s.DB.QueryContext(r.Context(), `
SELECT a.request_id, a.kind, a.resource_id, a.action, a.created_at
FROM management_operation_audit AS a
JOIN project_bindings AS b
  ON b.project_id=a.project_id AND b.cluster_id=a.cluster_id
JOIN management_project_grants AS g
  ON g.issuer=a.issuer AND g.subject=a.subject AND g.project_id=a.project_id
WHERE a.cluster_id=$1 AND a.project_id=$2
  AND a.issuer=$3 AND a.subject=$4
  AND g.role IN ('operator','admin')
ORDER BY a.created_at DESC, a.request_id DESC
LIMIT 26`, cluster, project, id.Issuer, id.Subject)
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	result := recentOperationList{Items: make([]recentOperation, 0, recentOperationLimit)}
	for rows.Next() {
		var item recentOperation
		if err := rows.Scan(&item.RequestID, &item.Kind, &item.ResourceID, &item.Action, &item.CommittedAt); err != nil {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if !operationID.MatchString(item.RequestID) || !safeID.MatchString(item.ResourceID) ||
			(item.Kind != "Workload" && item.Kind != "ComputePool") ||
			(item.Action != "create" && item.Action != "update" && item.Action != "delete-request") {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if len(result.Items) == recentOperationLimit {
			result.Truncated = true
			break
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
