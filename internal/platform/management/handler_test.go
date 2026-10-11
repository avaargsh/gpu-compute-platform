package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func oidcFixture(t *testing.T) (Introspector, *httptest.Server) {
	t.Helper()
	active := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("invalid introspection wire contract")
			w.WriteHeader(400)
			return
		}
		user, pw, ok := r.BasicAuth()
		if !ok || user != "manager" || pw != "secret" {
			w.WriteHeader(401)
			return
		}
		_ = r.ParseForm()
		token := r.Form.Get("token")
		answer := map[string]any{"active": active, "iss": "https://issuer.example", "sub": "alice", "aud": []string{"manager-api"}, "exp": time.Now().Add(time.Minute).Unix(), "token_type": "Bearer"}
		switch token {
		case "wrong-audience":
			answer["aud"] = "other-api"
		case "wrong-issuer":
			answer["iss"] = "https://attacker.example"
		case "expired-token":
			answer["exp"] = time.Now().Add(-time.Minute).Unix()
		case "revoked-token":
			answer["active"] = false
		case "missing-subject":
			delete(answer, "sub")
		case "bob-token":
			answer["sub"] = "bob"
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	return Introspector{URL: server.URL, Issuer: "https://issuer.example", Audience: "manager-api", ClientID: "manager", ClientSecret: "secret", AllowHTTPForTests: true}, server
}

func TestOIDCProviderIntrospectionDeniesForgedWrongAndRevokedTokens(t *testing.T) {
	verifier, server := oidcFixture(t)
	defer server.Close()
	if id, err := verifier.Verify(context.Background(), "good-access-token"); err != nil || id.Subject != "alice" {
		t.Fatalf("verified identity %+v err %v", id, err)
	}
	for _, token := range []string{"wrong-audience", "wrong-issuer", "expired-token", "revoked-token", "missing-subject", "short", "malformed token"} {
		if _, err := verifier.Verify(context.Background(), token); err == nil {
			t.Fatalf("accepted forbidden token %q", token)
		}
	}
	verifier.AllowHTTPForTests = false
	if _, err := verifier.Verify(context.Background(), "good-access-token"); err == nil {
		t.Fatal("production accepted plaintext introspection")
	}
}

func TestManagementRejectsUnconfiguredRoutesAndBearer(t *testing.T) {
	verifier, server := oidcFixture(t)
	defer server.Close()
	if _, err := NewHandler(nil, verifier); err == nil {
		t.Fatal("accepted nil postgres DB")
	}
	db := &sql.DB{}
	handler, err := NewHandler(db, verifier)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []struct {
		method, path, header string
		status               int
	}{
		{"GET", "/api/v1/agent/desired", "", 404},
		{"POST", "/api/v1/agent/register", "", 404},
		{"GET", "/api/v1/management/clusters/cluster-a/projects/proj/resources/Workload/job-a", "", 401},
		{"PUT", "/api/v1/management/clusters/cluster-a/projects/proj/resources/Workload/job-a", "Bearer short", 401},
		{"DELETE", "/api/v1/management/clusters/cluster-a/projects/proj/resources/Workload/../job-a", "", 404},
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(req.method, req.path, nil)
		if req.header != "" {
			r.Header.Set("Authorization", req.header)
		}
		handler.ServeHTTP(rec, r)
		if rec.Code != req.status {
			t.Errorf("%s %s: got %d want %d", req.method, req.path, rec.Code, req.status)
		}
	}
}

func TestManagementPostgresAuthorizationCASAndAtomicAudit(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires TEST_POSTGRES_DSN PostgreSQL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, path := range []string{"../../store/postgres/schema.sql", "schema.sql"} {
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(src)); err != nil {
			t.Fatal(err)
		}
	}
	suffix := time.Now().UnixNano()
	cluster := fmtID("cluster", suffix)
	project := fmtID("project", suffix)
	pool := fmtID("pool", suffix)
	job := fmtID("job", suffix)
	defer func() {
		for _, statement := range []string{
			"DELETE FROM management_operation_audit WHERE project_id=$1",
			"DELETE FROM management_project_grants WHERE project_id=$1",
			"DELETE FROM desired_resources WHERE cluster_id=$1",
			"DELETE FROM cluster_bindings WHERE cluster_id=$1",
			"DELETE FROM project_bindings WHERE project_id=$1",
		} {
			if strings.Contains(statement, "cluster_id=$1") {
				_, _ = db.ExecContext(ctx, statement, cluster)
			} else {
				_, _ = db.ExecContext(ctx, statement, project)
			}
		}
	}()
	for _, statement := range []struct {
		q    string
		args []any
	}{
		{"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", []any{project, cluster, project}},
		{"INSERT INTO cluster_bindings(pool_id,cluster_id,provider) VALUES($1,$2,'kueue')", []any{pool, cluster}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,$4)", []any{"https://issuer.example", "alice", project, "operator"}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,$4)", []any{"https://issuer.example", "bob", project, "viewer"}},
	} {
		if _, err := db.ExecContext(ctx, statement.q, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	verifier, idp := oidcFixture(t)
	defer idp.Close()
	handler, err := NewHandler(db, verifier)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	base := server.URL + "/api/v1/management/clusters/" + cluster + "/projects/" + project + "/resources/"
	issue := func(method, url, body, token, ifMatch string) (int, http.Header) {
		t.Helper()
		r, err := http.NewRequest(method, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if method == "PUT" {
			r.Header.Set("Content-Type", "application/json")
		}
		if ifMatch != "" {
			r.Header.Set("If-Match", ifMatch)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode, resp.Header
	}
	poolBody := `{"expectedGeneration":0,"spec":{"accelerators":[{"class":"h100","quota":2}],"acceleratorBindings":[{"class":"h100","resourceName":"nvidia.com/gpu","flavor":"h100"}],"scheduling":{"mode":"kueue"}}}`
	if status, _ := issue("PUT", base+"ComputePool/"+pool, poolBody, "", ""); status != 401 {
		t.Fatalf("anonymous write: %d", status)
	}
	if status, _ := issue("PUT", base+"ComputePool/"+pool, poolBody, "bob-token", ""); status != 403 {
		t.Fatalf("viewer write: %d", status)
	}
	foreignBase := server.URL + "/api/v1/management/clusters/" + cluster + "/projects/forbidden-project/resources/"
	if status, _ := issue("PUT", foreignBase+"ComputePool/"+pool, poolBody, "good-access-token", ""); status != 403 {
		t.Fatalf("foreign project create: %d", status)
	}
	if status, _ := issue("DELETE", foreignBase+"ComputePool/"+pool, "", "good-access-token", `"1"`); status != 403 {
		t.Fatalf("foreign project delete: %d", status)
	}
	if status, _ := issue("PUT", base+"ComputePool/"+pool, poolBody, "good-access-token", ""); status != 204 {
		t.Fatalf("create pool: %d", status)
	}
	if status, _ := issue("PUT", base+"ComputePool/"+pool, poolBody, "good-access-token", ""); status != 409 {
		t.Fatalf("stale create should be 409: %d", status)
	}
	jobBody := `{"expectedGeneration":0,"poolId":"` + pool + `","spec":{"image":"example.invalid/train:1","accelerator":{"class":"h100","quota":1}}}`
	if status, _ := issue("PUT", base+"Workload/"+job, jobBody, "good-access-token", ""); status != 204 {
		t.Fatalf("create work: %d", status)
	}
	if status, _ := issue("DELETE", base+"ComputePool/"+pool, "", "good-access-token", `"1"`); status != 409 {
		t.Fatalf("active workload allowed pool deletion: %d", status)
	}
	if status, _ := issue("DELETE", base+"Workload/"+job, "", "good-access-token", `"2"`); status != 409 {
		t.Fatalf("stale generation delete: %d", status)
	}
	if status, _ := issue("DELETE", base+"Workload/"+job, "", "good-access-token", `"1"`); status != 202 {
		t.Fatalf("delete work: %d", status)
	}
	var deleting bool
	if err := db.QueryRowContext(ctx, "SELECT deletion_timestamp IS NOT NULL FROM desired_resources WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2", cluster, job).Scan(&deleting); err != nil || !deleting {
		t.Fatalf("deleting=%t err=%v", deleting, err)
	}
	var audits int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM management_operation_audit WHERE project_id=$1", project).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}
	// A DB audit failure must rollback an otherwise successful generation update.
	if _, err := db.ExecContext(ctx, "ALTER TABLE management_operation_audit ADD CONSTRAINT management_audit_test_check_"+pool+" CHECK (false) NOT VALID"); err == nil {
		defer db.ExecContext(ctx, "ALTER TABLE management_operation_audit DROP CONSTRAINT management_audit_test_check_"+pool)
		poolUpdate := `{"expectedGeneration":1,"spec":{"accelerators":[{"class":"h100","quota":3}],"acceleratorBindings":[{"class":"h100","resourceName":"nvidia.com/gpu","flavor":"h100"}],"scheduling":{"mode":"kueue"}}}`
		if status, _ := issue("PUT", base+"ComputePool/"+pool, poolUpdate, "good-access-token", ""); status != 503 {
			t.Fatalf("audit failure status=%d", status)
		}
		var generation int64
		if err := db.QueryRowContext(ctx, "SELECT generation FROM desired_resources WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$2", cluster, pool).Scan(&generation); err != nil || generation != 1 {
			t.Fatalf("audit failure changed generation=%d err=%v", generation, err)
		}
	}
}

func fmtID(prefix string, n int64) string {
	return prefix + "-" + strconv.FormatInt(n, 10)
}
