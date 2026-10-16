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

func TestRecentOperationsCurrentSubjectProjectAndLimitPostgres(t *testing.T) {
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
	now := time.Now().UnixNano()
	cluster, project := fmtID("audit-cluster", now), fmtID("audit-project", now)
	otherProject := fmtID("audit-foreign", now)
	defer func() {
		for _, p := range []string{project, otherProject} {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM management_operation_audit WHERE project_id=$1", p)
			_, _ = db.ExecContext(context.Background(), "DELETE FROM management_project_grants WHERE project_id=$1", p)
			_, _ = db.ExecContext(context.Background(), "DELETE FROM project_bindings WHERE project_id=$1", p)
		}
	}()
	for _, p := range []string{project, otherProject} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", p, cluster, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []struct{ user, p, role string }{
		{"alice", project, "operator"}, {"bob", project, "viewer"},
		{"alice", otherProject, "operator"},
	} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,$4)",
			"https://issuer.example", grant.user, grant.p, grant.role); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(user, p, resource, action string, at time.Time) string {
		t.Helper()
		key, err := createRequestID()
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(ctx, `
INSERT INTO management_operation_audit
(request_id,request_digest,created_at,issuer,subject,project_id,cluster_id,kind,resource_id,action,
 expected_generation,resulting_generation,prior_spec_sha256,resulting_spec_sha256)
VALUES ($1,$2,$3,$4,$5,$6,$7,'Workload',$8,$9,0,1,$10,$11)`,
			key, strings.Repeat("a", 64), at, "https://issuer.example", user, p, cluster,
			resource, action, strings.Repeat("b", 64), strings.Repeat("c", 64))
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	origin := time.Now().Add(-time.Hour)
	var own []string
	for i := 0; i < 28; i++ {
		action := "create"
		if i%2 != 0 {
			action = "delete-request"
		}
		key := insert("alice", project, fmtID("job", int64(i+1)), action, origin.Add(time.Duration(i)*time.Minute))
		own = append(own, key)
	}
	// A late-committed transaction can have an older transaction-start
	// timestamp. The second page must still recover that operation ID.
	lateID := insert("alice", project, "late-commit", "create", origin.Add(-time.Minute))
	bobID := insert("bob", project, "bob-private", "create", origin.Add(40*time.Minute))
	foreignID := insert("alice", otherProject, "foreign-project", "create", origin.Add(41*time.Minute))
	auth, idp := oidcFixture(t)
	defer idp.Close()
	handler, err := NewHandler(db, auth)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/v1/management/clusters/" + cluster + "/projects/" + project + "/operations"
	call := func(url, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		token, url string
		status     int
	}{
		{"", endpoint, 401},
		{"revoked-token", endpoint, 401},
		{"provider-unavailable", endpoint, 503},
		{"good-access-token", endpoint + "?limit=999", 400},
		{"good-access-token", endpoint + "?after=!", 400},
		{"good-access-token", endpoint + "?after=abc&after=def", 400},
		{"good-access-token", strings.Replace(endpoint, "/projects/"+project+"/", "/projects/bad!id/", 1), 400},
	} {
		if got := call(tc.url, tc.token).Code; got != tc.status {
			t.Fatalf("token=%q url=%s status=%d want=%d", tc.token, tc.url, got, tc.status)
		}
	}
	result := call(endpoint, "good-access-token")
	if result.Code != 200 || result.Header().Get("Cache-Control") != "no-store" ||
		result.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("authorized read=%d headers=%v body=%s", result.Code, result.Header(), result.Body.String())
	}
	var list recentOperationList
	if err := json.Unmarshal(result.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 25 || !list.HasMore || list.NextCursor == nil ||
		list.Items[0].RequestID != own[27] || list.Items[24].RequestID != own[3] {
		t.Fatalf("bounded own-operation order: %+v", list)
	}
	second := call(endpoint+"?after="+*list.NextCursor, "good-access-token")
	if second.Code != 200 {
		t.Fatalf("older audit page=%d: %s", second.Code, second.Body.String())
	}
	var older recentOperationList
	if err := json.Unmarshal(second.Body.Bytes(), &older); err != nil {
		t.Fatal(err)
	}
	if len(older.Items) != 4 || older.HasMore || older.NextCursor != nil ||
		older.Items[0].RequestID != own[2] || older.Items[2].RequestID != own[0] ||
		older.Items[3].RequestID != lateID {
		t.Fatalf("late-committed older transaction-start row could not be recovered: %+v", older)
	}
	body := result.Body.String()
	for _, secret := range []string{bobID, foreignID, "requestDigest", "priorSpec", "resultingSpec", "issuer", "subject"} {
		if strings.Contains(body, secret) {
			t.Fatalf("operation discovery leaked %s", secret)
		}
	}
	if out := call(endpoint, "bob-token"); out.Code != 200 ||
		strings.Contains(out.Body.String(), bobID) || !strings.Contains(out.Body.String(), `"items":[]`) {
		t.Fatalf("viewer may not enumerate own or another operator receipts: %d %s", out.Code, out.Body.String())
	}
	foreign := call(strings.Replace(endpoint, "/projects/"+project+"/", "/projects/"+otherProject+"/", 1), "good-access-token")
	if foreign.Code != 200 || !strings.Contains(foreign.Body.String(), foreignID) ||
		strings.Contains(foreign.Body.String(), own[27]) {
		t.Fatalf("cross-project operations mixed: %d %s", foreign.Code, foreign.Body.String())
	}
	if _, err := db.ExecContext(ctx, "UPDATE management_project_grants SET role='viewer' WHERE project_id=$1 AND subject='alice'", project); err != nil {
		t.Fatal(err)
	}
	if out := call(endpoint, "good-access-token"); out.Code != 200 || len(out.Body.Bytes()) > 100 {
		t.Fatalf("downgraded operator kept receipt listing: %d %s", out.Code, out.Body.String())
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM management_project_grants WHERE project_id=$1 AND subject='alice'", project); err != nil {
		t.Fatal(err)
	}
	if out := call(endpoint, "good-access-token"); out.Code != 200 || len(out.Body.Bytes()) > 100 {
		t.Fatalf("revoked grant kept receipt listing: %d %s", out.Code, out.Body.String())
	}
}
