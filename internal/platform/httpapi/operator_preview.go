package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

// OperatorPreview is a temporary loopback-only, read-only operator session.
// It is not OIDC, user RBAC, or a deployable multi-tenant authentication system.
// In particular, existing Agent routes on the same server remain unauthenticated.
type OperatorPreview struct {
	Token     string
	ClusterID domain.ID
	Projects  []string
}

type operatorAPI struct {
	store    agentstore.Store
	tokenSHA [sha256.Size]byte
	cluster  domain.ID
	projects map[string]struct{}
}

type operatorRow struct {
	Desired agent.DesiredResource `json:"desired"`
	State   *resourceState        `json:"state"`
	Error   *string               `json:"error"`
}

type operatorSnapshot struct {
	Agent       agent.AgentStatus `json:"agent"`
	Rows        []operatorRow     `json:"rows"`
	Total       int               `json:"total"`
	Truncated   bool              `json:"truncated"`
	NextCursor  string            `json:"nextCursor,omitempty"`
	RefreshedAt time.Time         `json:"refreshedAt"`
}

// NewOperatorPreviewHandler adds one authenticated READ ONLY endpoint; callers
// must ensure both this handler and the older Agent APIs are loopback-only.
func NewOperatorPreviewHandler(store agentstore.Store, cfg OperatorPreview) (http.Handler, error) {
	if store == nil || len(cfg.Token) < 32 || cfg.ClusterID == "" || len(cfg.Projects) == 0 {
		return nil, errors.New("operator preview requires store, strong token, cluster and projects")
	}
	scope := make(map[string]struct{}, len(cfg.Projects))
	for _, project := range cfg.Projects {
		if project == "" || strings.TrimSpace(project) != project {
			return nil, errors.New("operator preview project scopes must be nonempty and normalized")
		}
		scope[project] = struct{}{}
	}
	api := &operatorAPI{store: store, tokenSHA: sha256.Sum256([]byte(cfg.Token)), cluster: cfg.ClusterID, projects: scope}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/operator/clusters/{clusterID}/snapshot", api.snapshot)
	return mux, nil
}

func (a *operatorAPI) authorize(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(hash[:], a.tokenSHA[:]) == 1
}

func projectScope(spec map[string]any) string {
	raw, ok := spec["projectID"]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	case domain.ID:
		return string(value)
	default:
		return ""
	}
}

func resourceKey(r agent.DesiredResource) string {
	return r.Kind + "/" + string(r.ID)
}

// inventoryFingerprint ties an opaque cursor to a complete, sorted,
// authorized Desired-resource collection. The operator preview is not a
// transactional snapshot: Observations can still change between page GETs.
// However, any mutation to visible desired state invalidates old cursors,
// including updates that leave Total unchanged.
func inventoryFingerprint(items []agent.DesiredResource) (string, error) {
	hash := sha256.New()
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return "", fmt.Errorf("encode resource inventory: %w", err)
		}
		_, _ = hash.Write(encoded)
		_, _ = hash.Write([]byte{10})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *operatorAPI) cursorSignature(after, fingerprint string) []byte {
	mac := hmac.New(sha256.New, a.tokenSHA[:])
	_, _ = mac.Write([]byte("operator-preview-v2\n" + string(a.cluster) + "\n" + after + "\n" + fingerprint))
	return mac.Sum(nil)
}

func (a *operatorAPI) nextCursor(after, fingerprint string) string {
	payload := after + "\n" + fingerprint + "\n" + base64.RawURLEncoding.EncodeToString(a.cursorSignature(after, fingerprint))
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// Returns HTTP 400 for malformed or forged cursors, HTTP 409 when a correctly
// signed cursor belongs to an obsolete Desired-state inventory.
func (a *operatorAPI) parseCursor(value, fingerprint string) (string, int) {
	if value == "" {
		return "", 0
	}
	if len(value) > 1024 {
		return "", http.StatusBadRequest
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", http.StatusBadRequest
	}
	parts := strings.Split(string(raw), "\n")
	if len(parts) != 3 || parts[0] == "" || len(parts[1]) != 64 {
		return "", http.StatusBadRequest
	}
	after, previousFingerprint, encodedSignature := parts[0], parts[1], parts[2]
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || !hmac.Equal(signature, a.cursorSignature(after, previousFingerprint)) {
		return "", http.StatusBadRequest
	}
	if previousFingerprint != fingerprint {
		return "", http.StatusConflict
	}
	return after, 0
}

func (a *operatorAPI) snapshot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !a.authorize(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "operator preview credentials required", http.StatusUnauthorized)
		return
	}
	if domain.ID(r.PathValue("clusterID")) != a.cluster {
		http.NotFound(w, r)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 120 {
			http.Error(w, "limit must be 1..120", http.StatusBadRequest)
			return
		}
		limit = n
	}
	status, found, err := a.store.GetAgentStatus(r.Context(), a.cluster)
	if err != nil {
		http.Error(w, "cluster state unavailable", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	resources, err := a.store.Desired(r.Context(), a.cluster)
	if err != nil {
		http.Error(w, "resource inventory unavailable", http.StatusInternalServerError)
		return
	}
	visible := make([]agent.DesiredResource, 0)
	seen := map[string]struct{}{}
	for _, item := range resources {
		if item.Kind != "ComputePool" && item.Kind != "Workload" {
			continue
		}
		if _, allowed := a.projects[projectScope(item.Spec)]; !allowed {
			continue
		}
		key := resourceKey(item)
		if item.ID == "" || item.Generation < 1 || strings.ContainsAny(key, "\r\n") {
			http.Error(w, "resource identity inconsistent", http.StatusConflict)
			return
		}
		if _, duplicate := seen[key]; duplicate {
			http.Error(w, "duplicate resource identity", http.StatusConflict)
			return
		}
		seen[key] = struct{}{}
		visible = append(visible, item)
	}
	sort.Slice(visible, func(i, j int) bool { return resourceKey(visible[i]) < resourceKey(visible[j]) })
	fingerprint, err := inventoryFingerprint(visible)
	if err != nil {
		http.Error(w, "resource inventory cannot be fingerprinted", http.StatusInternalServerError)
		return
	}
	after, cursorStatus := a.parseCursor(r.URL.Query().Get("cursor"), fingerprint)
	if cursorStatus != 0 {
		if cursorStatus == http.StatusConflict {
			http.Error(w, "resource inventory changed; restart pagination", http.StatusConflict)
		} else {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
		}
		return
	}
	out := operatorSnapshot{
		Agent: status, Rows: make([]operatorRow, 0, limit),
		Total: len(visible), RefreshedAt: time.Now().UTC(),
	}
	count := 0
	for _, item := range visible {
		if resourceKey(item) <= after {
			continue
		}
		if count == limit {
			out.Truncated = true
			break
		}
		observation, found, err := a.store.GetObservation(r.Context(), a.cluster, item.Kind, item.ID)
		if err != nil {
			http.Error(w, "resource observation unavailable", http.StatusInternalServerError)
			return
		}
		var state *resourceState
		if found {
			// Independent observation is descriptive, not admission/readiness proof.
			projected := projectResourceState(item.Generation, true, observation.ObservedGeneration, observation.Conditions, observation.EvidenceRefs)
			projected.Kind = item.Kind
			projected.ID = item.ID
			projected.ObservationLeaseOwner = observation.LeaseOwner
			state = &projected
		}
		out.Rows = append(out.Rows, operatorRow{Desired: item, State: state})
		count++
	}
	if out.Truncated && len(out.Rows) != 0 {
		out.NextCursor = a.nextCursor(resourceKey(out.Rows[len(out.Rows)-1].Desired), fingerprint)
	}
	writeJSON(w, http.StatusOK, out)
}

// NewRouterWithOperatorPreview keeps the legacy Agent routes unchanged, but
// puts the new operator endpoint behind an explicit server-side credential
// and project allowlist. Never expose this combined server outside loopback.
func NewRouterWithOperatorPreview(store agentstore.Store, bindings BindingStore, cfg OperatorPreview) (http.Handler, error) {
	operator, err := NewOperatorPreviewHandler(store, cfg)
	if err != nil {
		return nil, fmt.Errorf("configure operator preview: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/operator/", operator)
	mux.Handle("/", NewRouterWithDependencies(store, bindings))
	return previewBrowserWriteBoundary(mux), nil
}

// In the optional preview mode, a browser on a different origin must not be
// able to submit a simple HTML form to the otherwise-unmodified Agent API.
// Machine Agent JSON requests without Origin remain supported. This is only
// a CSRF/browser-content-type boundary, NOT machine authentication.
func previewBrowserWriteBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete) &&
			strings.HasPrefix(r.URL.Path, "/api/v1/") &&
			!strings.HasPrefix(r.URL.Path, "/api/v1/operator/") {
			if r.Header.Get("Origin") != "" || strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				http.Error(w, "browser-origin mutations are not allowed in operator preview", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodDelete {
				mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mediaType != "application/json" {
					http.Error(w, "operator preview requires application/json for machine mutations", http.StatusUnsupportedMediaType)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
