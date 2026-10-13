package management

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Project migration invalidates the previous cluster's management scope.
// DELETE must be fenced too: unlike PUT it does not invoke buildSpec.
func TestManagementProjectRebindRevokesPreviousClusterAuthority(t *testing.T) {
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
	for _, file := range []string{"../../store/postgres/schema.sql", "schema.sql"} {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}

	suffix := time.Now().UnixNano()
	oldCluster := fmtID("oldcluster", suffix)
	newCluster := fmtID("newcluster", suffix)
	project := fmtID("migratedproject", suffix)
	pool := fmtID("migratedpool", suffix)
	defer func() {
		for _, row := range []struct{ sql, key string }{
			{"DELETE FROM management_operation_audit WHERE project_id=$1", project},
			{"DELETE FROM management_project_grants WHERE project_id=$1", project},
			{"DELETE FROM desired_resources WHERE cluster_id=$1", oldCluster},
			{"DELETE FROM cluster_bindings WHERE cluster_id=$1", oldCluster},
			{"DELETE FROM project_bindings WHERE project_id=$1", project},
		} {
			_, _ = db.ExecContext(ctx, row.sql, row.key)
		}
	}()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", []any{project, oldCluster, project}},
		{"INSERT INTO cluster_bindings(pool_id,cluster_id,provider) VALUES($1,$2,'kueue')", []any{pool, oldCluster}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,'operator')", []any{"https://issuer.example", "alice", project}},
	} {
		if _, err := db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}

	auth, idp := oidcFixture(t)
	defer idp.Close()
	handler, err := NewHandler(db, auth)
	if err != nil {
		t.Fatal(err)
	}
	resource := "/api/v1/management/clusters/" + oldCluster + "/projects/" + project + "/resources/ComputePool/" + pool
	poolBody := "{\"expectedGeneration\":0,\"spec\":{\"accelerators\":[{\"class\":\"h100\",\"quota\":2}],\"acceleratorBindings\":[{\"class\":\"h100\",\"resourceName\":\"nvidia.com/gpu\",\"flavor\":\"h100\"}],\"scheduling\":{\"mode\":\"kueue\"}}}"
	createID, err := createRequestID()
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, requestID, match string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer good-access-token")
		if method == http.MethodPut {
			req.Header.Set("Content-Type", "application/json")
		}
		if requestID != "" {
			req.Header.Set("Idempotency-Key", requestID)
		}
		if match != "" {
			req.Header.Set("If-Match", match)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if status := call(http.MethodPut, resource, poolBody, createID, ""); status != http.StatusNoContent {
		t.Fatalf("initial authorized pool create: HTTP %d", status)
	}
	receipt := "/api/v1/management/clusters/" + oldCluster + "/projects/" + project + "/operations/" + createID
	if status := call(http.MethodGet, resource, "", "", ""); status != http.StatusOK {
		t.Fatalf("initial resource read: HTTP %d", status)
	}
	if status := call(http.MethodGet, receipt, "", "", ""); status != http.StatusOK {
		t.Fatalf("initial operation receipt: HTTP %d", status)
	}

	// The same operator grant remains, but the current cluster binding moves.
	if _, err := db.ExecContext(ctx,
		"UPDATE project_bindings SET cluster_id=$1 WHERE project_id=$2",
		newCluster, project,
	); err != nil {
		t.Fatal(err)
	}
	if status := call(http.MethodGet, resource, "", "", ""); status != http.StatusForbidden {
		t.Fatalf("old-cluster read leaked after rebind: HTTP %d", status)
	}
	deleteID, err := createRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if status := call(http.MethodDelete, resource, "", deleteID, "\"1\""); status != http.StatusForbidden {
		t.Fatalf("old-cluster DELETE authorized after rebind: HTTP %d", status)
	}
	putID, err := createRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if status := call(http.MethodPut, resource, poolBody, putID, ""); status != http.StatusForbidden {
		t.Fatalf("old-cluster PUT authorized after rebind: HTTP %d", status)
	}
	if status := call(http.MethodGet, receipt, "", "", ""); status != http.StatusNotFound {
		t.Fatalf("old-cluster receipt leaked after rebind: HTTP %d", status)
	}
	var generation, audited int
	var deleting bool
	err = db.QueryRowContext(ctx,
		"SELECT generation, deletion_timestamp IS NOT NULL FROM desired_resources WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$2",
		oldCluster, pool,
	).Scan(&generation, &deleting)
	if err != nil || generation != 1 || deleting {
		t.Fatalf("old desired state changed: generation=%d deleting=%t err=%v", generation, deleting, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM management_operation_audit WHERE project_id=$1", project).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("unauthorized mutation produced audit: count=%d err=%v", audited, err)
	}
}
