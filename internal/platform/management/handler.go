package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var operationID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Service struct {
	DB                        *sql.DB
	Auth                      Introspector
	AllowedBrowserOrigin      string
	RequireRuntimeDBAuthority bool
}

type mutationInput struct {
	ExpectedGeneration *int64          `json:"expectedGeneration"`
	PoolID             string          `json:"poolId,omitempty"`
	Spec               json.RawMessage `json:"spec"`
}

// NewHandler keeps the non-browser CLI default: no Origin-bearing writes.
func NewHandler(db *sql.DB, auth Introspector) (http.Handler, error) {
	return NewHandlerWithBrowserOrigin(db, auth, "")
}

// NewHandlerWithBrowserOrigin opts into one exact loopback browser origin.
// The Origin is a CSRF/browser boundary only, NOT identity or project auth:
// each request still requires a verified bearer and DB-owned RBAC.
func NewHandlerWithBrowserOrigin(db *sql.DB, auth Introspector, origin string) (http.Handler, error) {
	return newHandlerWithAuthorityCheck(db, auth, origin, false)
}

// NewGuardedHandlerWithBrowserOrigin is the production management entrypoint.
// Unlike the test-only and legacy handler constructors it requires a fresh
// runtime Postgres authority check before EVERY desired-state mutation.
func NewGuardedHandlerWithBrowserOrigin(db *sql.DB, auth Introspector, origin string) (http.Handler, error) {
	return newHandlerWithAuthorityCheck(db, auth, origin, true)
}

func newHandlerWithAuthorityCheck(db *sql.DB, auth Introspector, origin string, guard bool) (http.Handler, error) {
	if err := validateBrowserOrigin(origin); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("PostgreSQL is required; no in-memory management mutations")
	}
	if err := auth.ValidateConfig(); err != nil {
		return nil, err
	}
	svc := &Service{DB: db, Auth: auth, AllowedBrowserOrigin: origin, RequireRuntimeDBAuthority: guard}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/management/projects", svc.projects)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(204)
	})
	// Readiness is NOT process liveness. It proves that the effective
	// database login can still perform the required project-scoped
	// transactions, including a fresh least-privilege authorization check.
	// It does not test the identity provider or Kubernetes execution.
	mux.HandleFunc("GET /readyz", svc.readiness)
	mux.HandleFunc("GET /api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}", svc.serve)
	mux.HandleFunc("PUT /api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}", svc.serve)
	mux.HandleFunc("DELETE /api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}", svc.serve)
	mux.HandleFunc("GET /api/v1/management/clusters/{clusterID}/projects/{projectID}/operations/{operationID}", svc.receipt)
	return mux, nil
}

func problem(w http.ResponseWriter, status int) {
	// Do not relay SQL errors or IdP details to callers.
	http.Error(w, http.StatusText(status), status)
}

func createRequestID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// Restrict the pilot to an explicit numeric loopback Origin, including
// a separately tested same-host HTTPS static gateway. Never treat a valid
// HTTPS Origin as proof of safe public ingress, identity or authorization.
func validateBrowserOrigin(origin string) error {
	if origin == "" {
		return nil // CLI only (no Origin header); browser writes remain blocked.
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != "127.0.0.1" ||
		u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" ||
		u.Port() == "" || u.String() != origin {
		return errors.New("MANAGEMENT_BROWSER_ORIGIN must be an exact http(s)://127.0.0.1:<port>")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("MANAGEMENT_BROWSER_ORIGIN has invalid loopback port")
	}
	return nil
}

func (s *Service) browserWriteAllowed(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return true // CLI without browser Origin, still OIDC + project RBAC.
	}
	return len(values) == 1 && s.AllowedBrowserOrigin != "" && values[0] == s.AllowedBrowserOrigin
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cluster, project, kind, id := r.PathValue("clusterID"), r.PathValue("projectID"), r.PathValue("kind"), r.PathValue("resourceID")
	if !safeID.MatchString(cluster) || !safeID.MatchString(project) || !safeID.MatchString(id) || (kind != "Workload" && kind != "ComputePool") {
		problem(w, 400)
		return
	}
	token := bearerToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		problem(w, 401)
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
	if r.Method == http.MethodGet {
		s.read(w, r, identity, cluster, project, kind, id)
		return
	}
	if !s.browserWriteAllowed(r) {
		problem(w, 403)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && r.Method == http.MethodPut {
		problem(w, 415)
		return
	}
	// A caller-chosen cryptographically random 128-bit operation ID enables
	// safe replay after a lost HTTP response. A duplicate ID must carry the
	// identical operation digest, or the transaction rejects it.
	requestID := r.Header.Get("Idempotency-Key")
	if requestID == "" {
		problem(w, http.StatusPreconditionRequired)
		return
	}
	if !operationID.MatchString(requestID) || len(r.Header.Values("Idempotency-Key")) != 1 {
		problem(w, http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Request-ID", requestID)
	var input mutationInput
	if r.Method == http.MethodPut {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			problem(w, 400)
			return
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			problem(w, 400)
			return
		}
		if input.ExpectedGeneration == nil || *input.ExpectedGeneration < 0 || len(input.Spec) == 0 {
			problem(w, 400)
			return
		}
	} else {
		match := r.Header.Get("If-Match")
		if len(match) < 3 || !strings.HasPrefix(match, "\"") || !strings.HasSuffix(match, "\"") {
			problem(w, 428)
			return
		}
		n, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
		if err != nil || n < 1 {
			problem(w, 400)
			return
		}
		input.ExpectedGeneration = &n
	}
	status, gen := s.mutate(r.Context(), identity, cluster, project, kind, id, input, r.Method, requestID)
	if status != 0 {
		problem(w, status)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", gen))
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusAccepted)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// Authorization requires both the CURRENT project grant and the CURRENT
// project-to-cluster binding. A grant for a migrated project must not allow
// stale resource reads or DELETEs in its previous cluster.
func (s *Service) role(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id Identity, cluster, project string, lock bool) (string, error) {
	query := `SELECT g.role FROM management_project_grants AS g
JOIN project_bindings AS b ON b.project_id=g.project_id AND b.cluster_id=$4
WHERE g.issuer=$1 AND g.subject=$2 AND g.project_id=$3`
	if lock {
		// PostgreSQL requires UPDATE privilege for SELECT ... FOR SHARE.
		// Keep the API LOGIN's grants read-only on authorization tables:
		// the DBA-owned, fixed-query SECURITY DEFINER function alone takes
		// shared row locks on the verified grant and project binding.
		query = "SELECT role FROM public.management_locked_project_role($1,$2,$3,$4) AS locked(role)"
	}
	var role string
	err := q.QueryRowContext(ctx, query, id.Issuer, id.Subject, project, cluster).Scan(&role)
	return role, err
}

func (s *Service) read(w http.ResponseWriter, r *http.Request, id Identity, cluster, project, kind, resourceID string) {
	role, err := s.role(r.Context(), s.DB, id, cluster, project, false)
	if err == sql.ErrNoRows {
		problem(w, 403)
		return
	}
	if err != nil {
		problem(w, 503)
		return
	}
	if role != "viewer" && role != "operator" && role != "admin" {
		problem(w, 403)
		return
	}
	var spec []byte
	var gen int64
	var deletion sql.NullTime
	// Re-check binding AND live grant in the *same snapshot* as the resource
	// read. The preliminary role check above preserves the 403 contract,
	// but cannot alone prevent rebind/revoke TOCTOU between two statements.
	err = s.DB.QueryRowContext(r.Context(), `
SELECT d.generation, d.spec, d.deletion_timestamp
FROM desired_resources AS d
JOIN project_bindings AS b
  ON b.project_id=$4 AND b.cluster_id=d.cluster_id
JOIN management_project_grants AS g
  ON g.project_id=b.project_id
WHERE d.cluster_id=$1 AND d.kind=$2 AND d.resource_id=$3
  AND d.spec->>'projectID'=$4
  AND g.issuer=$5 AND g.subject=$6
  AND g.role IN ('viewer', 'operator', 'admin')
`, cluster, kind, resourceID, project, id.Issuer, id.Subject).Scan(&gen, &spec, &deletion)
	if err == sql.ErrNoRows {
		problem(w, 404)
		return
	}
	if err != nil {
		problem(w, 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", gen))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"clusterId": cluster, "projectId": project, "kind": kind, "id": resourceID,
		"generation": gen, "spec": json.RawMessage(spec), "deleting": deletion.Valid,
	})
}

func specHash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// mutationFingerprint contains every API-authorized input including the
// exact expected Generation. It intentionally does not include token text.
func mutationFingerprint(cluster, project, kind, resourceID string, in mutationInput, method string) (string, error) {
	payload, err := json.Marshal(struct {
		Method             string          `json:"method"`
		Cluster            string          `json:"cluster"`
		Project            string          `json:"project"`
		Kind               string          `json:"kind"`
		ResourceID         string          `json:"resourceId"`
		ExpectedGeneration int64           `json:"expectedGeneration"`
		PoolID             string          `json:"poolId"`
		Spec               json.RawMessage `json:"spec,omitempty"`
	}{
		Method: method, Cluster: cluster, Project: project, Kind: kind,
		ResourceID: resourceID, ExpectedGeneration: *in.ExpectedGeneration,
		PoolID: in.PoolID, Spec: in.Spec,
	})
	if err != nil {
		return "", err
	}
	return specHash(payload), nil
}

func (s *Service) mutate(ctx context.Context, id Identity, cluster, project, kind, resourceID string, in mutationInput, method, requestID string) (int, int64) {
	// Do not rely only on the startup-time privilege snapshot. Re-read the
	// actual configured LOGIN authority for every authorized write, including
	// idempotency replays. Fail closed before opening a write transaction.
	if s.RequireRuntimeDBAuthority {
		if err := VerifyRuntimeDBPrivileges(ctx, s.DB); err != nil {
			return http.StatusServiceUnavailable, 0
		}
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 503, 0
	}
	defer tx.Rollback()
	role, err := s.role(ctx, tx, id, cluster, project, true)
	if err == sql.ErrNoRows {
		return 403, 0
	}
	if err != nil {
		return 503, 0
	}
	if role != "operator" && role != "admin" {
		return 403, 0
	}
	// Bind the same client operation ID to the exact authenticated action.
	// The same ID + same payload can be replayed after a lost response; ID
	// reuse across users, projects, resources or different payloads is 409.
	fingerprint, err := mutationFingerprint(cluster, project, kind, resourceID, in, method)
	if err != nil {
		return 400, 0
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "management-request/"+requestID); err != nil {
		return 503, 0
	}
	var priorIssuer, priorSubject, priorProject, priorCluster, priorKind, priorResource, priorAction, priorDigest string
	var priorExpected, priorGeneration int64
	err = tx.QueryRowContext(ctx, `SELECT issuer,subject,project_id,cluster_id,kind,resource_id,action,expected_generation,resulting_generation,request_digest
 FROM management_operation_audit WHERE request_id=$1`, requestID).Scan(
		&priorIssuer, &priorSubject, &priorProject, &priorCluster, &priorKind, &priorResource,
		&priorAction, &priorExpected, &priorGeneration, &priorDigest)
	if err != nil && err != sql.ErrNoRows {
		return 503, 0
	}
	if err == nil {
		actionMatches := (method == http.MethodDelete && priorAction == "delete-request") ||
			(method == http.MethodPut && (priorAction == "create" || priorAction == "update"))
		if priorIssuer != id.Issuer || priorSubject != id.Subject || priorProject != project ||
			priorCluster != cluster || priorKind != kind || priorResource != resourceID ||
			priorExpected != *in.ExpectedGeneration || priorDigest != fingerprint || !actionMatches {
			return 409, 0
		}
		return 0, priorGeneration
	}

	// Use the same lifecycle advisory key as the Agent Store; serialize
	// absent-row create with concurrent delete/finalize, not only existing rows.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "desired/"+cluster+"/"+kind+"/"+resourceID); err != nil {
		return 503, 0
	}
	if kind == "Workload" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "Workload/"+resourceID); err != nil {
			return 503, 0
		}
	}

	var previousGen int64
	var previousSpec []byte
	var deletion sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT generation,spec,deletion_timestamp FROM desired_resources WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 FOR UPDATE`, cluster, kind, resourceID).Scan(&previousGen, &previousSpec, &deletion)
	if err != nil && err != sql.ErrNoRows {
		return 503, 0
	}
	exists := err == nil
	if exists {
		var old struct {
			ProjectID string `json:"projectID"`
		}
		if json.Unmarshal(previousSpec, &old) != nil || old.ProjectID != project {
			return 404, 0
		}
	}
	expected := *in.ExpectedGeneration
	if !exists && expected != 0 {
		return 409, 0
	}
	if exists && previousGen != expected {
		return 409, 0
	}
	if deletion.Valid {
		return 409, 0
	}
	if method == http.MethodDelete && !exists {
		return 404, 0
	}
	if method == http.MethodPut && kind == "Workload" {
		// The existing Agent Store treats Workload IDs as globally canonical.
		// Hold the same Workload/<id> advisory key and reject a cross-cluster
		// incarnation before creating another Desired resource.
		var crossCluster int
		err := tx.QueryRowContext(ctx, `SELECT count(*) FROM desired_resources WHERE kind='Workload' AND resource_id=$1 AND cluster_id<>$2`, resourceID, cluster).Scan(&crossCluster)
		if err != nil {
			return 503, 0
		}
		if crossCluster != 0 {
			return 409, 0
		}
	}
	if method == http.MethodPut && kind == "ComputePool" && exists {
		// Active children rely on immutable parent accelerator bindings;
		// do not change the pool's provider/bindings underneath them.
		var children int
		err := tx.QueryRowContext(ctx, `SELECT count(*) FROM desired_resources WHERE cluster_id=$1 AND kind='Workload' AND spec->>'poolID'=$2`, cluster, resourceID).Scan(&children)
		if err != nil {
			return 503, 0
		}
		if children != 0 {
			return 409, 0
		}
	}
	if method == http.MethodPut && kind == "Workload" && exists {
		return 409, 0
	} // immutable Workload: delete/recreate only

	var newSpec []byte
	var newGen int64
	action := "delete-request"
	if method == http.MethodPut {
		if !exists {
			var tombstone int64
			er := tx.QueryRowContext(ctx, `SELECT generation FROM deletion_tombstones WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3`, cluster, kind, resourceID).Scan(&tombstone)
			if er == nil {
				return 409, 0
			} // avoid reusing finalized identity through this initial API
			if er != sql.ErrNoRows {
				return 503, 0
			}
		}
		newSpec, err = s.buildSpec(ctx, tx, cluster, project, kind, resourceID, in)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 409, 0
			}
			if errors.Is(err, errInvalidSpec) {
				return 400, 0
			}
			if errors.Is(err, errBinding) {
				return 409, 0
			}
			return 503, 0
		}
		if exists {
			newGen = previousGen + 1
			action = "update"
		} else {
			newGen = 1
			action = "create"
		}
		if newGen < 1 {
			return 409, 0
		}
		if exists {
			result, e := tx.ExecContext(ctx, `UPDATE desired_resources SET generation=$4,spec=$5,updated_at=now()
     WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 AND generation=$6 AND deletion_timestamp IS NULL`,
				cluster, kind, resourceID, newGen, newSpec, expected)
			if e != nil {
				return 503, 0
			}
			n, e := result.RowsAffected()
			if e != nil {
				return 503, 0
			}
			if n != 1 {
				return 409, 0
			}
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO desired_resources (cluster_id,kind,resource_id,generation,spec) VALUES ($1,$2,$3,$4,$5)`, cluster, kind, resourceID, newGen, newSpec)
			if err != nil {
				return 409, 0
			}
		}
	} else {
		newGen = previousGen
		newSpec = previousSpec
		if kind == "ComputePool" {
			// Refuse pool teardown while workloads still refer to it.
			var n int
			e := tx.QueryRowContext(ctx, `SELECT count(*) FROM desired_resources WHERE cluster_id=$1 AND kind='Workload' AND spec->>'poolID'=$2`, cluster, resourceID).Scan(&n)
			if e != nil {
				return 503, 0
			}
			if n != 0 {
				return 409, 0
			}
		}
		finalizers, _ := json.Marshal([]string{agentstore.ProviderCleanupFinalizer})
		result, e := tx.ExecContext(ctx, `UPDATE desired_resources SET deletion_timestamp=now(),
   finalizers=CASE WHEN finalizers ? $4 THEN finalizers ELSE finalizers || $5::jsonb END,
   updated_at=now() WHERE cluster_id=$1 AND kind=$2 AND resource_id=$3 AND generation=$6 AND deletion_timestamp IS NULL`,
			cluster, kind, resourceID, agentstore.ProviderCleanupFinalizer, string(finalizers), previousGen)
		if e != nil {
			return 503, 0
		}
		n, e := result.RowsAffected()
		if e != nil {
			return 503, 0
		}
		if n != 1 {
			return 409, 0
		}
	}
	// This append-only audit event commits atomically with the Desired change.
	// An audit insert failure rolls back the *entire* write and CAS outcome.
	_, err = tx.ExecContext(ctx, `INSERT INTO management_operation_audit
 (request_id,request_digest,issuer,subject,project_id,cluster_id,kind,resource_id,action,expected_generation,resulting_generation,prior_spec_sha256,resulting_spec_sha256)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		requestID, fingerprint, id.Issuer, id.Subject, project, cluster, kind, resourceID, action, expected, newGen, specHash(previousSpec), specHash(newSpec))
	if err != nil {
		return 503, 0
	}
	if err := tx.Commit(); err != nil {
		// Commit outcome can be uncertain after a transport failure; never
		// falsely report an authoritative CAS conflict. The caller may safely
		// retry with the SAME Idempotency-Key and identical request.
		return 503, 0
	}
	return 0, newGen
}

var errInvalidSpec = errors.New("invalid resource spec")
var errBinding = errors.New("invalid resource binding")

func (s *Service) buildSpec(ctx context.Context, tx *sql.Tx, cluster, project, kind, resourceID string, in mutationInput) ([]byte, error) {
	var projectCluster, namespace string
	// mutate() already called the DBA-owned locked_project_role function
	// in this SAME serializable transaction, holding FOR SHARE on this
	// project's binding row through COMMIT. This ordinary SELECT avoids
	// granting UPDATE authority to the Management API login.
	err := tx.QueryRowContext(ctx, `SELECT cluster_id,namespace FROM project_bindings WHERE project_id=$1`, project).Scan(&projectCluster, &namespace)
	if err != nil {
		return nil, err
	}
	if projectCluster != cluster {
		return nil, errBinding
	}
	var spec map[string]any
	if kind == "ComputePool" {
		var pool domain.ComputePoolSpec
		if err := strictJSON(in.Spec, &pool); err != nil {
			return nil, errInvalidSpec
		}
		if len(pool.Accelerators) == 0 || len(pool.AcceleratorBindings) == 0 || pool.Scheduling.Mode != "kueue" {
			return nil, errInvalidSpec
		}
		classes := map[string]bool{}
		for _, binding := range pool.AcceleratorBindings {
			if binding.Class == "" || binding.ResourceName == "" || binding.Flavor == "" || binding.AllocationMode == domain.AcceleratorAllocationDRA {
				return nil, errInvalidSpec
			}
			classes[binding.Class] = true
		}
		for _, acc := range pool.Accelerators {
			if acc.Class == "" || acc.Quota <= 0 || !classes[acc.Class] {
				return nil, errInvalidSpec
			}
		}
		var poolCluster, provider string
		e := tx.QueryRowContext(ctx, `SELECT cluster_id,provider FROM public.management_locked_pool_binding($1)`, resourceID).Scan(&poolCluster, &provider)
		if e != nil {
			return nil, e
		}
		if poolCluster != cluster || provider != "kueue" {
			return nil, errBinding
		}
		spec = map[string]any{"projectID": project, "namespace": namespace, "provider": provider, "accelerators": pool.Accelerators, "acceleratorBindings": pool.AcceleratorBindings, "scheduling": pool.Scheduling}
	} else {
		var work domain.WorkloadSpec
		if err := strictJSON(in.Spec, &work); err != nil {
			return nil, errInvalidSpec
		}
		if !safeID.MatchString(in.PoolID) || work.Image == "" || work.Accelerator.Class == "" || work.Accelerator.Quota <= 0 {
			return nil, errInvalidSpec
		}
		var poolCluster, provider string
		e := tx.QueryRowContext(ctx, `SELECT cluster_id,provider FROM public.management_locked_pool_binding($1)`, in.PoolID).Scan(&poolCluster, &provider)
		if e != nil {
			return nil, e
		}
		if poolCluster != cluster || provider != "kueue" {
			return nil, errBinding
		}
		var poolSpec []byte
		var deleting sql.NullTime
		e = tx.QueryRowContext(ctx, `SELECT spec,deletion_timestamp FROM desired_resources WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$2 FOR SHARE`, cluster, in.PoolID).Scan(&poolSpec, &deleting)
		if e != nil {
			return nil, e
		}
		if deleting.Valid {
			return nil, errBinding
		}
		var parent struct {
			ProjectID string                      `json:"projectID"`
			Bindings  []domain.AcceleratorBinding `json:"acceleratorBindings"`
		}
		if json.Unmarshal(poolSpec, &parent) != nil || parent.ProjectID != project {
			return nil, errBinding
		}
		matched := false
		for _, b := range parent.Bindings {
			if b.Class == work.Accelerator.Class {
				matched = true
			}
		}
		if !matched {
			return nil, errBinding
		}
		spec = map[string]any{"projectID": project, "poolID": in.PoolID, "namespace": namespace, "provider": provider, "image": work.Image, "command": work.Command, "accelerator": work.Accelerator}
	}
	return json.Marshal(spec)
}

func strictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errInvalidSpec
	}
	return nil
}
