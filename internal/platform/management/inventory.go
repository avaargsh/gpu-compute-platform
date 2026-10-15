package management

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

// Workload inventory deliberately exposes only Desired metadata. Neither a
// listing nor a Generation proves Kueue admission, execution or cleanup.
// Fixed-size keyset pages prevent unbounded project-wide response dumps.
type workloadInventoryItem struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
	Deleting   bool   `json:"deleting"`
}

type workloadInventoryPage struct {
	Items      []workloadInventoryItem `json:"items"`
	NextCursor *string                 `json:"nextCursor"`
	HasMore    bool                    `json:"hasMore"`
}

const workloadInventoryPageSize = 25

func (s *Service) workloadInventory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cluster, project := r.PathValue("clusterID"), r.PathValue("projectID")
	if !safeID.MatchString(cluster) || !safeID.MatchString(project) {
		problem(w, http.StatusBadRequest)
		return
	}
	params := r.URL.Query()
	var after string
	if len(params) > 1 {
		problem(w, http.StatusBadRequest)
		return
	}
	values, exists := params["after"]
	if len(params) == 1 && !exists {
		problem(w, http.StatusBadRequest)
		return
	}
	if exists {
		if len(values) != 1 || !safeID.MatchString(values[0]) {
			problem(w, http.StatusBadRequest)
			return
		}
		after = values[0]
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
		} else {
			w.Header().Set("WWW-Authenticate", "Bearer")
			problem(w, http.StatusUnauthorized)
		}
		return
	}
	// Preserve the existing 403 contract for absent live project grants.
	// The actual inventory query ALSO joins the grant/binding in the SAME
	// database statement as the Desired read, avoiding a role-check TOCTOU.
	role, err := s.role(r.Context(), s.DB, identity, cluster, project, false)
	if err == sql.ErrNoRows {
		problem(w, http.StatusForbidden)
		return
	}
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	if role != "viewer" && role != "operator" && role != "admin" {
		problem(w, http.StatusForbidden)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
SELECT d.resource_id, d.generation, (d.deletion_timestamp IS NOT NULL)
FROM desired_resources AS d
JOIN project_bindings AS b ON b.project_id=$3 AND b.cluster_id=d.cluster_id
JOIN management_project_grants AS g
  ON g.project_id=b.project_id AND g.issuer=$4 AND g.subject=$5
  AND g.role IN ('viewer','operator','admin')
WHERE d.cluster_id=$1 AND d.kind='Workload'
  AND d.resource_id>$2 AND d.spec->>'projectID'=$3
ORDER BY d.resource_id
LIMIT 26`, cluster, after, project, identity.Issuer, identity.Subject)
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	page := workloadInventoryPage{Items: make([]workloadInventoryItem, 0, workloadInventoryPageSize)}
	for rows.Next() {
		var item workloadInventoryItem
		if err := rows.Scan(&item.ID, &item.Generation, &item.Deleting); err != nil {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if item.Generation < 1 || !safeID.MatchString(item.ID) {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if len(page.Items) == workloadInventoryPageSize {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	if page.HasMore {
		last := page.Items[len(page.Items)-1].ID
		page.NextCursor = &last
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}
