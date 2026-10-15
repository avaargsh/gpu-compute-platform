package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestWorkloadInventoryPageBoundariesWithPostgres(t *testing.T) {
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
	suffix := time.Now().UnixNano()
	cluster := fmt.Sprintf("inventory-cluster-%d", suffix)
	project := fmt.Sprintf("inventory-project-%d", suffix)
	otherProject := fmt.Sprintf("inventory-other-%d", suffix)
	defer func() {
		// No TRUNCATE: this test may share a disposable PostgreSQL instance.
		for _, statement := range []struct{ q, key string }{
			{"DELETE FROM desired_resources WHERE cluster_id=$1", cluster},
			{"DELETE FROM management_project_grants WHERE project_id=$1", project},
			{"DELETE FROM management_project_grants WHERE project_id=$1", otherProject},
			{"DELETE FROM project_bindings WHERE project_id=$1", project},
			{"DELETE FROM project_bindings WHERE project_id=$1", otherProject},
		} {
			if _, err := db.ExecContext(context.Background(), statement.q, statement.key); err != nil {
				t.Errorf("cleanup inventory fixture: %v", err)
			}
		}
	}()
	for _, p := range []string{project, otherProject} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", p, cluster, p); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES('https://issuer.example','bob',$1,'viewer')", p); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 28; i++ {
		name := fmt.Sprintf("job-%02d", i)
		var deletion any
		if i == 1 {
			deletion = time.Now().UTC()
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO desired_resources(cluster_id,kind,resource_id,generation,spec,deletion_timestamp)
VALUES($1,'Workload',$2,$3,$4::jsonb,$5)`,
			cluster, name, i+1, `{"projectID":"`+project+`","image":"private-sentinel"}`, deletion); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range []struct{ kind, name, p string }{
		{"Workload", "foreign-job", otherProject},
		{"ComputePool", "pool-a", project},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO desired_resources(cluster_id,kind,resource_id,generation,spec)
VALUES($1,$2,$3,1,$4::jsonb)`,
			cluster, extra.kind, extra.name, `{"projectID":"`+extra.p+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	auth, idp := oidcFixture(t)
	defer idp.Close()
	handler, err := NewHandler(db, auth)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/management/clusters/" + cluster + "/projects/" + project + "/workloads"
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
		token, endpoint string
		status          int
	}{
		{"", path, 401},
		{"revoked-token", path, 401},
		{"provider-unavailable", path, 503},
		{"good-access-token", path, 403}, // Alice has no grant
		{"bob-token", strings.Replace(path, "/projects/"+project+"/", "/projects/missing/", 1), 403},
		{"bob-token", path + "?after=", 400},
		{"bob-token", path + "?after=../other", 400},
		{"bob-token", path + "?after=job-01&after=job-02", 400},
		{"bob-token", path + "?limit=100000", 400},
	} {
		rec := read(tc.token, tc.endpoint)
		if rec.Code != tc.status {
			t.Fatalf("token=%s path=%s got=%d want=%d: %s", tc.token, tc.endpoint, rec.Code, tc.status, rec.Body.String())
		}
	}
	first := read("bob-token", path)
	if first.Code != 200 {
		t.Fatalf("first page: %d %s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), "private-sentinel") || strings.Contains(first.Body.String(), "issuer") {
		t.Fatal("inventory leaked full private Workload spec or caller identity")
	}
	var page workloadInventoryPage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 25 || !page.HasMore || page.NextCursor == nil || *page.NextCursor != "job-24" {
		t.Fatalf("invalid first page: %+v", page)
	}
	if page.Items[0].ID != "job-00" || page.Items[1].ID != "job-01" || !page.Items[1].Deleting {
		t.Fatalf("sorted identity/deletion not preserved: %+v", page.Items[:2])
	}
	second := read("bob-token", path+"?after="+*page.NextCursor)
	if second.Code != 200 {
		t.Fatalf("second page: %d %s", second.Code, second.Body.String())
	}
	var next workloadInventoryPage
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 3 || next.HasMore || next.NextCursor != nil || next.Items[0].ID != "job-25" {
		t.Fatalf("invalid terminal page: %+v", next)
	}
	foreign := read("bob-token", strings.Replace(path, "/projects/"+project+"/", "/projects/"+otherProject+"/", 1))
	var foreignPage workloadInventoryPage
	if foreign.Code != 200 || json.Unmarshal(foreign.Body.Bytes(), &foreignPage) != nil ||
		len(foreignPage.Items) != 1 || foreignPage.Items[0].ID != "foreign-job" {
		t.Fatalf("cross-project list leaked or omitted rows: %d %s", foreign.Code, foreign.Body.String())
	}
	// Revocation between pages cannot expose a previously authorized cursor.
	if _, err := db.ExecContext(ctx, "DELETE FROM management_project_grants WHERE subject='bob' AND project_id=$1", project); err != nil {
		t.Fatal(err)
	}
	if rec := read("bob-token", path+"?after=job-24"); rec.Code != 403 {
		t.Fatalf("revoked Project grant still permitted second page: %d", rec.Code)
	}
}
