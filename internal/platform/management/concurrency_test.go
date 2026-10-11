package management

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Contenders race on the *same* Desired identity with different operation IDs.
// Exactly one write may commit at each expected Generation; only committed
// writes receive audit receipts, including absent-row create races.
func TestManagementPostgresConcurrentCASAndAuditReceipts(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires TEST_POSTGRES_DSN PostgreSQL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	ctx := context.Background()
	for _, file := range []string{"../../store/postgres/schema.sql", "schema.sql"} {
		raw, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	suffix := time.Now().UnixNano()
	cluster, project, pool := fmtID("racecluster", suffix), fmtID("raceproject", suffix), fmtID("racepool", suffix)
	defer func() {
		for _, item := range []struct {
			sql string
			key string
		}{
			{"DELETE FROM management_operation_audit WHERE project_id=$1", project},
			{"DELETE FROM management_project_grants WHERE project_id=$1", project},
			{"DELETE FROM desired_resources WHERE cluster_id=$1", cluster},
			{"DELETE FROM cluster_bindings WHERE cluster_id=$1", cluster},
			{"DELETE FROM project_bindings WHERE project_id=$1", project},
		} {
			_, _ = db.ExecContext(ctx, item.sql, item.key)
		}
	}()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", []any{project, cluster, project}},
		{"INSERT INTO cluster_bindings(pool_id,cluster_id,provider) VALUES($1,$2,'kueue')", []any{pool, cluster}},
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
	server := httptest.NewServer(handler)
	defer server.Close()
	resourceURL := fmt.Sprintf("%s/api/v1/management/clusters/%s/projects/%s/resources/ComputePool/%s", server.URL, cluster, project, pool)
	poolBody := func(expected, quota int) string {
		return fmt.Sprintf(`{"expectedGeneration":%d,"spec":{"accelerators":[{"class":"h100","quota":%d}],"acceleratorBindings":[{"class":"h100","resourceName":"nvidia.com/gpu","flavor":"h100"}],"scheduling":{"mode":"kueue"}}}`, expected, quota)
	}
	client := &http.Client{Timeout: 12 * time.Second}
	burst := func(expected int) {
		t.Helper()
		const n = 8
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan int, n)
		for i := 0; i < n; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				operationID, err := createRequestID()
				if err != nil {
					results <- 0
					return
				}
				req, err := http.NewRequest(http.MethodPut, resourceURL, strings.NewReader(poolBody(expected, 2+i)))
				if err != nil {
					results <- 0
					return
				}
				req.Header.Set("Authorization", "Bearer good-access-token")
				req.Header.Set("Idempotency-Key", operationID)
				req.Header.Set("Content-Type", "application/json")
				<-start
				resp, err := client.Do(req)
				if err != nil {
					results <- 0
					return
				}
				resp.Body.Close()
				results <- resp.StatusCode
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		success := 0
		for status := range results {
			switch status {
			case http.StatusNoContent:
				success++
			case http.StatusConflict, http.StatusServiceUnavailable:
				// Serializable conflicts may report a retryable 503; the
				// caller uses the SAME idempotency key when retrying.
			default:
				t.Errorf("unexpected contention status %d", status)
			}
		}
		if success != 1 {
			t.Fatalf("expected exactly one commit at generation %d, got %d", expected, success)
		}
	}
	burst(0)
	burst(1)
	var generation int64
	if err := db.QueryRowContext(ctx, "SELECT generation FROM desired_resources WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$2", cluster, pool).Scan(&generation); err != nil || generation != 2 {
		t.Fatalf("concurrent writes resulted in generation %d, err %v", generation, err)
	}
	var audited int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM management_operation_audit WHERE project_id=$1", project).Scan(&audited); err != nil || audited != 2 {
		t.Fatalf("expected only two atomic audit receipts for two commits, got %d err %v", audited, err)
	}
}
