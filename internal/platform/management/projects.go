package management

import (
	"encoding/json"
	"errors"
	"net/http"
)

// ProjectAccess is DB-owned scope metadata, never a client-supplied
// authorization claim. Every resource operation re-checks the current grant.
type ProjectAccess struct {
	ProjectID string `json:"projectId"`
	ClusterID string `json:"clusterId"`
	Role      string `json:"role"`
}

type ProjectList struct {
	Projects  []ProjectAccess `json:"projects"`
	Truncated bool            `json:"truncated"`
}

// projects lists only scopes with BOTH a live grant for this exact
// introspected issuer+subject AND a project binding. Unbound/orphaned grants
// never grant discoverability. It is intentionally read-only and capped.
func (s *Service) projects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
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

	// LIMIT 101 distinguishes a bounded first page from a complete list.
	// Never claim that the returned first 100 include all memberships.
	rows, err := s.DB.QueryContext(r.Context(), `
SELECT g.project_id, b.cluster_id, g.role
FROM management_project_grants AS g
JOIN project_bindings AS b ON b.project_id=g.project_id
WHERE g.issuer=$1 AND g.subject=$2
  AND g.role IN ('viewer','operator','admin')
ORDER BY g.project_id, b.cluster_id
LIMIT 101`, id.Issuer, id.Subject)
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()

	result := ProjectList{Projects: make([]ProjectAccess, 0)}
	for rows.Next() {
		var project ProjectAccess
		if err := rows.Scan(&project.ProjectID, &project.ClusterID, &project.Role); err != nil {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if len(result.Projects) >= 100 {
			result.Truncated = true
			break
		}
		result.Projects = append(result.Projects, project)
	}
	if err := rows.Err(); err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
