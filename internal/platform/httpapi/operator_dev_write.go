package httpapi

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

const maxOperatorDevWorkloadBytes = 64 << 10

var devWorkloadID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type operatorDevWriter struct {
	reader    operatorAPI
	writeSHA  [sha256.Size]byte
	store     agentstore.Store
	placement BindingStore
}

// NewOperatorDevWriteHandler is a separate development-only capability.
// The read-only operator credential alone can never create a Workload.
func NewOperatorDevWriteHandler(store agentstore.Store, bindings BindingStore, cfg OperatorPreview) (http.Handler, error) {
	if store == nil || bindings == nil || !cfg.DevelopmentWrites ||
		len(cfg.Token) < 32 || len(cfg.DevelopmentWriteToken) < 32 ||
		cfg.Token == cfg.DevelopmentWriteToken || cfg.ClusterID == "" ||
		len(cfg.Projects) == 0 {
		return nil, errors.New("local write needs development mode and distinct strong read/write tokens")
	}
	allowed := make(map[string]struct{}, len(cfg.Projects))
	for _, project := range cfg.Projects {
		if project == "" || strings.TrimSpace(project) != project || !devWorkloadID.MatchString(project) {
			return nil, errors.New("local write scopes require normalized project IDs")
		}
		allowed[project] = struct{}{}
	}
	api := &operatorDevWriter{
		reader: operatorAPI{
			tokenSHA: sha256.Sum256([]byte(cfg.Token)), cluster: cfg.ClusterID, projects: allowed,
		},
		writeSHA: sha256.Sum256([]byte(cfg.DevelopmentWriteToken)),
		store:    store, placement: bindings,
	}
	return http.HandlerFunc(api.putWorkload), nil
}

func (a *operatorDevWriter) authorizedWriteToken(r *http.Request) bool {
	values := r.Header.Values("X-Operator-Write-Token")
	if len(values) != 1 || len(values[0]) < 32 || strings.ContainsAny(values[0], " \t\r\n") {
		return false
	}
	tokenSHA := sha256.Sum256([]byte(values[0]))
	return subtle.ConstantTimeCompare(tokenSHA[:], a.writeSHA[:]) == 1
}

func (a *operatorDevWriter) putWorkload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !a.reader.authorize(r) || !a.authorizedWriteToken(r) {
		http.Error(w, "local development write authorization required", http.StatusUnauthorized)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" &&
		origin != "http://127.0.0.1:3001" && origin != "http://localhost:3001" {
		http.Error(w, "nonlocal browser mutation not permitted", http.StatusForbidden)
		return
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		http.Error(w, "cross-site browser mutation not permitted", http.StatusForbidden)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "JSON Workload required", http.StatusUnsupportedMediaType)
		return
	}
	cluster := domain.ID(r.PathValue("clusterID"))
	project := domain.ID(r.PathValue("projectID"))
	workloadID := domain.ID(r.PathValue("resourceID"))
	_, allowed := a.reader.projects[string(project)]
	if cluster != a.reader.cluster || !allowed || !devWorkloadID.MatchString(string(workloadID)) {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxOperatorDevWorkloadBytes+1))
	if err != nil || len(raw) > maxOperatorDevWorkloadBytes {
		http.Error(w, "invalid or oversized Workload JSON", http.StatusRequestEntityTooLarge)
		return
	}
	var input domain.Workload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid Workload", http.StatusBadRequest)
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		http.Error(w, "multiple JSON documents prohibited", http.StatusBadRequest)
		return
	}
	if input.Metadata.ID != workloadID || input.Metadata.Generation < 1 ||
		input.ProjectID != project || input.PoolID == "" {
		http.Error(w, "Workload identity does not match authorized route", http.StatusBadRequest)
		return
	}
	projectPlacement, err := a.placement.ResolveProject(r.Context(), project)
	if err != nil || projectPlacement.ClusterID != cluster {
		http.Error(w, "project is not bound to the authorized cluster", http.StatusConflict)
		return
	}
	poolPlacement, err := a.placement.ResolvePool(r.Context(), input.PoolID)
	if err != nil || poolPlacement.ClusterID != cluster {
		http.Error(w, "pool is not bound to the authorized cluster", http.StatusConflict)
		return
	}
	pool, found, err := a.store.GetDesired(r.Context(), cluster, "ComputePool", input.PoolID)
	if err != nil {
		http.Error(w, "pool identity unavailable", http.StatusInternalServerError)
		return
	}
	if !found || pool.DeletionTimestamp != nil || projectScope(pool.Spec) != string(project) {
		http.Error(w, "ComputePool not owned by scoped project", http.StatusConflict)
		return
	}
	// Re-serialize the validated typed struct to eliminate ambiguous raw JSON.
	// Delegate to the existing atomic generation/parent admission, never to a
	// parallel ad hoc Desired write or a separate scheduler.
	canonical, err := json.Marshal(input)
	if err != nil {
		http.Error(w, "invalid Workload encoding", http.StatusBadRequest)
		return
	}
	forward := r.Clone(r.Context())
	forward.Body = io.NopCloser(bytes.NewReader(canonical))
	forward.SetPathValue("resourceID", string(workloadID))
	NewResourceAPI(a.store, a.placement).UpsertWorkload(w, forward)
}
