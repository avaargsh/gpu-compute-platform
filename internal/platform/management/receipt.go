package management

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// OperationReceipt is an immutable *Desired-state commit* receipt, not proof
// of Kubernetes admission, successful reconciliation or resource readiness.
// The subject and project are checked against live project grants at query
// time; a prior authorization can never bypass membership revocation.
type OperationReceipt struct {
	RequestID           string    `json:"requestId"`
	Status              string    `json:"status"`
	ClusterID           string    `json:"clusterId"`
	ProjectID           string    `json:"projectId"`
	Kind                string    `json:"kind"`
	ResourceID          string    `json:"resourceId"`
	Action              string    `json:"action"`
	ExpectedGeneration  int64     `json:"expectedGeneration"`
	ResultingGeneration int64     `json:"resultingGeneration"`
	RequestDigest       string    `json:"requestDigest"`
	CommittedAt         time.Time `json:"committedAt"`
}

func (s *Service) receipt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	cluster, project, key := r.PathValue("clusterID"), r.PathValue("projectID"), r.PathValue("operationID")
	if !safeID.MatchString(cluster) || !safeID.MatchString(project) || !operationID.MatchString(key) {
		problem(w, http.StatusBadRequest)
		return
	}

	token := bearerToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		problem(w, http.StatusUnauthorized)
		return
	}
	identity, err := s.Auth.Verify(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrIntrospectionUnavailable) {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		problem(w, http.StatusUnauthorized)
		return
	}

	var out OperationReceipt
	// A single SQL statement enforces the ORIGINAL verified subject,
	// cluster, project and the CURRENT role. Operators cannot enumerate
	// other operators' receipts, even within the same project. A forbidden
	// operation and a nonexistent operation both return 404.
	err = s.DB.QueryRowContext(r.Context(), `
SELECT a.kind, a.resource_id, a.action, a.expected_generation,
       a.resulting_generation, a.request_digest, a.created_at
FROM management_operation_audit AS a
JOIN management_project_grants AS g
  ON g.issuer=a.issuer AND g.subject=a.subject AND g.project_id=a.project_id
WHERE a.request_id=$1 AND a.cluster_id=$2 AND a.project_id=$3
  AND a.issuer=$4 AND a.subject=$5
  AND g.role IN ('operator','admin')
`, key, cluster, project, identity.Issuer, identity.Subject).Scan(
		&out.Kind, &out.ResourceID, &out.Action, &out.ExpectedGeneration,
		&out.ResultingGeneration, &out.RequestDigest, &out.CommittedAt,
	)
	if err == sql.ErrNoRows {
		problem(w, http.StatusNotFound)
		return
	}
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}

	out.RequestID = key
	out.Status = "desired-committed"
	out.ClusterID = cluster
	out.ProjectID = project
	w.Header().Set("X-Request-ID", key)
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", out.ResultingGeneration))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
