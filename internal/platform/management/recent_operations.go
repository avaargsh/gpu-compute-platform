package management

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const recentOperationLimit = 25

// The audit timestamp is transaction-start time, NOT commit order. A keyset
// cursor lets an operator page further back even if a late-committed operation
// was started earlier than the 25 rows visible on the first page.
type recentOperation struct {
	RequestID  string    `json:"requestId"`
	Kind       string    `json:"kind"`
	ResourceID string    `json:"resourceId"`
	Action     string    `json:"action"`
	RecordedAt time.Time `json:"recordedAt"`
}

type recentOperationList struct {
	Items      []recentOperation `json:"items"`
	NextCursor *string           `json:"nextCursor"`
	HasMore    bool              `json:"hasMore"`
}

func encodeOperationCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeOperationCursor(value string) (time.Time, string, bool) {
	if value == "" || len(value) > 256 {
		return time.Time{}, "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 160 {
		return time.Time{}, "", false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 || !operationID.MatchString(parts[1]) {
		return time.Time{}, "", false
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || at.IsZero() {
		return time.Time{}, "", false
	}
	return at, parts[1], true
}

// This GET lists only the exact current OIDC issuer+subject's audited writes
// in a currently granted and bound Project. Each fixed 25-row page is a
// NON-SNAPSHOT hint, not an immutable receipt or a permit to replay writes.
// A selected original operation ID must be verified via the receipt endpoint.
func (s *Service) recentOperations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cluster, project := r.PathValue("clusterID"), r.PathValue("projectID")
	if !safeID.MatchString(cluster) || !safeID.MatchString(project) {
		problem(w, http.StatusBadRequest)
		return
	}
	params := r.URL.Query()
	if len(params) > 1 {
		problem(w, http.StatusBadRequest)
		return
	}
	cursor, hasCursor := params["after"]
	if len(params) == 1 && !hasCursor {
		problem(w, http.StatusBadRequest)
		return
	}
	var before time.Time
	var beforeID string
	if hasCursor {
		var ok bool
		if len(cursor) != 1 {
			problem(w, http.StatusBadRequest)
			return
		}
		before, beforeID, ok = decodeOperationCursor(cursor[0])
		if !ok {
			problem(w, http.StatusBadRequest)
			return
		}
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
	// Both paths use a fixed SQL statement; user input is always bound.
	// The owner-prefixed index supports full equality scoping + keyset ORDER.
	query := `
SELECT a.request_id, a.kind, a.resource_id, a.action, a.created_at
FROM management_operation_audit AS a
JOIN project_bindings AS b
  ON b.project_id=a.project_id AND b.cluster_id=a.cluster_id
JOIN management_project_grants AS g
  ON g.issuer=a.issuer AND g.subject=a.subject AND g.project_id=a.project_id
WHERE a.issuer=$1 AND a.subject=$2
  AND a.project_id=$3 AND a.cluster_id=$4
  AND g.role IN ('operator','admin')`
	args := []any{id.Issuer, id.Subject, project, cluster}
	if hasCursor {
		query += " AND (a.created_at, a.request_id) < ($5,$6)"
		args = append(args, before, beforeID)
	}
	query += " ORDER BY a.created_at DESC, a.request_id DESC LIMIT 26"
	rows, err := s.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	out := recentOperationList{Items: make([]recentOperation, 0, recentOperationLimit)}
	for rows.Next() {
		var item recentOperation
		if err := rows.Scan(&item.RequestID, &item.Kind, &item.ResourceID, &item.Action, &item.RecordedAt); err != nil {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if !operationID.MatchString(item.RequestID) || !safeID.MatchString(item.ResourceID) ||
			(item.Kind != "Workload" && item.Kind != "ComputePool") ||
			(item.Action != "create" && item.Action != "update" && item.Action != "delete-request") {
			problem(w, http.StatusServiceUnavailable)
			return
		}
		if len(out.Items) == recentOperationLimit {
			out.HasMore = true
			break
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	if out.HasMore {
		last := out.Items[len(out.Items)-1]
		next := encodeOperationCursor(last.RecordedAt, last.RequestID)
		out.NextCursor = &next
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
