package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/authn"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

// ProjectMembership is backed by a trusted database, not by roles, scopes,
// project IDs or membership assertions in the incoming browser token.
type ProjectMembership interface {
	CanReadProject(context.Context, authn.Principal, domain.ID) (bool, error)
}

type ProjectReadAPI struct {
	store      agentstore.Store
	placement  PlacementResolver
	membership ProjectMembership
	verifier   authn.Verifier
}

func NewProjectReadRouter(store agentstore.Store, placement PlacementResolver, membership ProjectMembership, verifier authn.Verifier) (http.Handler, error) {
	if store == nil || placement == nil || membership == nil || verifier == nil {
		return nil, errors.New("project read API requires store, placement, membership and token verifier")
	}
	api := &ProjectReadAPI{store: store, placement: placement, membership: membership, verifier: verifier}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /api/v1/projects/{projectID}/compute-pools", api.list("ComputePool"))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/workloads", api.list("Workload"))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/compute-pools/{resourceID}", api.detail("ComputePool"))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/workloads/{resourceID}", api.detail("Workload"))
	return mux, nil
}

func (a *ProjectReadAPI) authorize(w http.ResponseWriter, r *http.Request) (domain.ID, Placement, bool) {
	projectID := domain.ID(r.PathValue("projectID"))
	if projectID == "" {
		http.NotFound(w, r)
		return "", Placement{}, false
	}
	// Reject duplicate Authorization headers, Basic, cookies and unverified
	// user headers. Only a token validated by the provider is authoritative.
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
		http.Error(w, "missing verified access token", http.StatusUnauthorized)
		return "", Placement{}, false
	}
	token := strings.TrimPrefix(headers[0], "Bearer ")
	principal, err := a.verifier.Verify(r.Context(), token)
	if err != nil || principal.Issuer == "" || principal.Subject == "" {
		http.Error(w, "invalid access token", http.StatusUnauthorized)
		return "", Placement{}, false
	}
	allowed, err := a.membership.CanReadProject(r.Context(), principal, projectID)
	if err != nil {
		http.Error(w, "project authorization unavailable", http.StatusServiceUnavailable)
		return "", Placement{}, false
	}
	if !allowed {
		http.NotFound(w, r) // do not reveal foreign Project existence
		return "", Placement{}, false
	}
	placement, err := a.placement.ResolveProject(r.Context(), projectID)
	if err != nil || placement.ClusterID == "" {
		http.NotFound(w, r)
		return "", Placement{}, false
	}
	return projectID, placement, true
}

func desiredID(value any) domain.ID {
	switch v := value.(type) {
	case string:
		return domain.ID(v)
	case domain.ID:
		return v
	default:
		return ""
	}
}

func desiredBelongsToProject(item agent.DesiredResource, projectID domain.ID) bool {
	return projectID != "" && desiredID(item.Spec["projectID"]) == projectID
}

func (a *ProjectReadAPI) projectView(ctx context.Context, kind string, item agent.DesiredResource, clusterID domain.ID) (any, error) {
	obs, found, err := a.store.GetObservation(ctx, clusterID, kind, item.ID)
	if err != nil {
		return nil, err
	}
	state := projectResourceState(item.Generation, found, obs.ObservedGeneration, obs.Conditions, obs.EvidenceRefs)
	state.Kind = kind
	state.ID = item.ID
	if kind == "Workload" {
		view, err := projectWorkload(item)
		view.Status = state
		return view, err
	}
	view, err := projectComputePool(item)
	view.Status = state
	return view, err
}

func (a *ProjectReadAPI) inProjectCluster(ctx context.Context, item agent.DesiredResource, kind string, clusterID domain.ID) bool {
	poolID := item.ID
	if kind == "Workload" {
		poolID = desiredID(item.Spec["poolID"])
		if poolID == "" {
			return false
		}
	}
	binding, err := a.placement.ResolvePool(ctx, poolID)
	return err == nil && binding.ClusterID == clusterID
}

func (a *ProjectReadAPI) detail(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, placement, ok := a.authorize(w, r)
		if !ok {
			return
		}
		id := domain.ID(r.PathValue("resourceID"))
		if id == "" {
			http.NotFound(w, r)
			return
		}
		// Read ONLY the authorized Project's cluster; never LocateDesired
		// globally and then expose whether a foreign resource exists.
		item, found, err := a.store.GetDesired(r.Context(), placement.ClusterID, kind, id)
		if err != nil {
			http.Error(w, "resource read unavailable", http.StatusServiceUnavailable)
			return
		}
		if !found || !desiredBelongsToProject(item, projectID) ||
			!a.inProjectCluster(r.Context(), item, kind, placement.ClusterID) {
			http.NotFound(w, r)
			return
		}
		view, err := a.projectView(r.Context(), kind, item, placement.ClusterID)
		if err != nil {
			http.Error(w, "resource projection unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

type pageCursor struct {
	Version int       `json:"v"`
	Project domain.ID `json:"project"`
	Kind    string    `json:"kind"`
	After   domain.ID `json:"after"`
}

func readPage(r *http.Request, project domain.ID, kind string) (int, domain.ID, error) {
	size := 20
	after := domain.ID("")
	query := r.URL.Query()
	if len(query["pageSize"]) > 1 || len(query["pageToken"]) > 1 {
		return 0, "", errors.New("duplicate paging parameter")
	}
	if values := query["pageSize"]; len(values) == 1 {
		number, err := strconv.Atoi(values[0])
		if err != nil || number < 1 || number > 100 {
			return 0, "", errors.New("invalid page size")
		}
		size = number
	}
	if values := query["pageToken"]; len(values) == 1 {
		if len(values[0]) == 0 || len(values[0]) > 1024 {
			return 0, "", errors.New("invalid page cursor")
		}
		raw, err := base64.RawURLEncoding.DecodeString(values[0])
		if err != nil || len(raw) > 512 {
			return 0, "", errors.New("invalid page cursor")
		}
		var cursor pageCursor
		if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 ||
			cursor.Project != project || cursor.Kind != kind || cursor.After == "" {
			return 0, "", errors.New("cursor does not match the project")
		}
		after = cursor.After
	}
	return size, after, nil
}

func encodePage(project domain.ID, kind string, after domain.ID) string {
	bytes, _ := json.Marshal(pageCursor{Version: 1, Project: project, Kind: kind, After: after})
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func (a *ProjectReadAPI) list(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, placement, ok := a.authorize(w, r)
		if !ok {
			return
		}
		size, after, err := readPage(r, projectID, kind)
		if err != nil {
			http.Error(w, "invalid pagination", http.StatusBadRequest)
			return
		}
		items, err := a.store.Desired(r.Context(), placement.ClusterID)
		if err != nil {
			http.Error(w, "resource list unavailable", http.StatusServiceUnavailable)
			return
		}
		filtered := make([]agent.DesiredResource, 0)
		for _, item := range items {
			if item.Kind == kind && desiredBelongsToProject(item, projectID) &&
				string(item.ID) > string(after) &&
				a.inProjectCluster(r.Context(), item, kind, placement.ClusterID) {
				filtered = append(filtered, item)
			}
		}
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
		hasMore := len(filtered) > size
		if hasMore {
			filtered = filtered[:size]
		}
		views := make([]any, 0, len(filtered))
		for _, item := range filtered {
			view, err := a.projectView(r.Context(), kind, item, placement.ClusterID)
			if err != nil {
				http.Error(w, "resource projection unavailable", http.StatusServiceUnavailable)
				return
			}
			views = append(views, view)
		}
		next := ""
		if hasMore && len(filtered) > 0 {
			next = encodePage(projectID, kind, filtered[len(filtered)-1].ID)
		}
		writeJSON(w, http.StatusOK, struct {
			Items []any  `json:"items"`
			Next  string `json:"nextPageToken,omitempty"`
		}{Items: views, Next: next})
	}
}
