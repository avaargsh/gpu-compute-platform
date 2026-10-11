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

func TestProjectsDiscoveryCurrentOIDCSubjectAndRevokedGrantPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires TEST_POSTGRES_DSN")
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
	aliceProject := fmtID("alice-project", suffix)
	bobProject := fmtID("bob-project", suffix)
	unboundProject := fmtID("orphan-project", suffix)
	cluster := fmtID("discovery-cluster", suffix)
	defer func() {
		for _, project := range []string{aliceProject, bobProject, unboundProject} {
			_, _ = db.ExecContext(ctx, "DELETE FROM management_project_grants WHERE project_id=$1", project)
			_, _ = db.ExecContext(ctx, "DELETE FROM project_bindings WHERE project_id=$1", project)
		}
	}()
	for _, project := range []string{aliceProject, bobProject} {
		if _, err := db.ExecContext(ctx, "INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", project, cluster, project); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{subject, project, role string}{
		{"alice", aliceProject, "operator"},
		{"bob", bobProject, "viewer"},
		{"alice", unboundProject, "admin"},
	} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,$4)",
			"https://issuer.example", row.subject, row.project, row.role); err != nil {
			t.Fatal(err)
		}
	}
	auth, idp := oidcFixture(t)
	defer idp.Close()
	handler, err := NewHandler(db, auth)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	query := func(token string) (int, ProjectList, http.Header) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/management/projects", nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result ProjectList
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, result, resp.Header
	}
	if status, _, _ := query(""); status != 401 {
		t.Fatalf("anonymous project discovery: %d", status)
	}
	if status, _, _ := query("revoked-token"); status != 401 {
		t.Fatalf("revoked OIDC token: %d", status)
	}
	if status, _, _ := query("provider-unavailable"); status != 503 {
		t.Fatalf("IdP outage: %d", status)
	}
	if status, list, headers := query("good-access-token"); status != 200 ||
		headers.Get("Cache-Control") != "no-store" || list.Truncated || len(list.Projects) != 1 ||
		list.Projects[0].ProjectID != aliceProject || list.Projects[0].ClusterID != cluster ||
		list.Projects[0].Role != "operator" {
		t.Fatalf("alice leaked hidden/unbound memberships: status=%d projects=%+v", status, list)
	}
	if status, list, _ := query("bob-token"); status != 200 || len(list.Projects) != 1 ||
		list.Projects[0].ProjectID != bobProject || list.Projects[0].Role != "viewer" {
		t.Fatalf("bob project discovery failed: status=%d projects=%+v", status, list)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM management_project_grants WHERE issuer=$1 AND subject=$2 AND project_id=$3",
		"https://issuer.example", "alice", aliceProject); err != nil {
		t.Fatal(err)
	}
	if status, list, _ := query("good-access-token"); status != 200 || len(list.Projects) != 0 || list.Truncated {
		t.Fatalf("revoked membership still discoverable: status=%d %+v", status, list)
	}
	// Discovery never registers old machine agent routes and never accepts
	// caller-supplied project IDs as authority.
	req := httptest.NewRequest("GET", "/api/v1/agent/desired", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("legacy Agent route exposed: %d", rec.Code)
	}
}
