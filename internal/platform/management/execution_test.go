package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestExecutionConditionsFailClosed(t *testing.T) {
	for _, value := range []string{
		"null", "{}", "[{\"type\":\"Ready\",\"status\":\"True\"},{\"type\":\"Ready\",\"status\":\"False\"}]",
		"[{\"type\":\"Ready\",\"status\":\"yes\"}]",
		"[{\"type\":\"\",\"status\":\"True\"}]",
	} {
		if _, ok := decodeExecutionConditions([]byte(value)); ok {
			t.Fatalf("accepted bad conditions %q", value)
		}
	}
	if _, ok := decodeExecutionConditions([]byte(`[{"type":"Admitted","status":"True","reason":"Kueue"}]`)); !ok {
		t.Fatal("rejected valid Kueue condition")
	}
}

func TestManagementExecutionProjectAuthorizationAndGenerationWithPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable TEST_POSTGRES_DSN")
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
	cluster := fmtID("exec-cluster", time.Now().UnixNano())
	project := fmtID("exec-project", time.Now().UnixNano())
	resource := fmtID("exec-workload", time.Now().UnixNano())
	defer func() {
		// Only disposable fixture rows are removed; never touch another test.
		for _, item := range []struct {
			query string
			arg   string
		}{
			{"DELETE FROM resource_observations WHERE cluster_id=$1", cluster},
			{"DELETE FROM desired_resources WHERE cluster_id=$1", cluster},
			{"DELETE FROM management_project_grants WHERE project_id=$1", project},
			{"DELETE FROM project_bindings WHERE project_id=$1", project},
		} {
			if _, err := db.ExecContext(context.Background(), item.query, item.arg); err != nil {
				t.Errorf("cleanup temporary execution fixture: %v", err)
			}
		}
	}()
	for _, item := range []struct {
		q    string
		args []any
	}{
		{"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", []any{project, cluster, project}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,'viewer')", []any{"https://issuer.example", "bob", project}},
		{"INSERT INTO desired_resources(cluster_id,kind,resource_id,generation,spec) VALUES($1,'Workload',$2,3,$3::jsonb)", []any{cluster, resource, `{"projectID":"` + project + `"}`}},
	} {
		if _, err := db.ExecContext(ctx, item.q, item.args...); err != nil {
			t.Fatal(err)
		}
	}
	auth, idp := oidcFixture(t)
	defer idp.Close()
	handler, err := NewHandler(db, auth)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/management/clusters/" + cluster + "/projects/" + project + "/execution/workloads/" + resource
	read := func(token, endpoint string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		token  string
		path   string
		status int
	}{
		{"", path, 401},
		{"revoked-token", path, 401},
		{"provider-unavailable", path, 503},
		{"good-access-token", path, 404}, // alice has no project grant
		{"bob-token", strings.Replace(path, "/projects/"+project+"/", "/projects/foreign/", 1), 404},
		{"bob-token", strings.Replace(path, "/workloads/"+resource, "/workloads/foreign", 1), 404},
	} {
		if got := read(tc.token, tc.path).Code; got != tc.status {
			t.Fatalf("token=%s status=%d want=%d", tc.token, got, tc.status)
		}
	}
	first := read("bob-token", path)
	if first.Code != 200 {
		t.Fatalf("viewer scoped read=%d body=%s", first.Code, first.Body.String())
	}
	var out struct {
		DesiredGeneration  int64                `json:"desiredGeneration"`
		ObservedGeneration *int64               `json:"observedGeneration"`
		Conditions         []executionCondition `json:"conditions"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.DesiredGeneration != 3 || out.ObservedGeneration != nil || len(out.Conditions) != 0 {
		t.Fatalf("no observation must remain unknown, got %+v", out)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_observations
	(cluster_id,kind,resource_id,observed_generation,conditions,observed_at)
	VALUES($1,'Workload',$2,2,$3::jsonb,now())`, cluster, resource,
		`[{"type":"QuotaReserved","status":"True"},{"type":"Admitted","status":"False","reason":"Pending"}]`); err != nil {
		t.Fatal(err)
	}
	second := read("bob-token", path)
	if second.Code != 200 {
		t.Fatalf("observed read=%d %s", second.Code, second.Body.String())
	}
	if err := json.Unmarshal(second.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ObservedGeneration == nil || *out.ObservedGeneration != 2 || len(out.Conditions) != 2 {
		t.Fatalf("must expose distinct stale observed generation, got %+v", out)
	}
	// Revoke current authorization; function must not leak the observation.
	if _, err := db.ExecContext(ctx, "DELETE FROM management_project_grants WHERE subject='bob' AND project_id=$1", project); err != nil {
		t.Fatal(err)
	}
	if got := read("bob-token", path).Code; got != 404 {
		t.Fatalf("revoked grant leaked execution status=%d", got)
	}
}
