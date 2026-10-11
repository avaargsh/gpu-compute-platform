package main

import (
	"bytes"
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

	"github.com/avaargsh/gpu-compute-platform/internal/platform/management"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Real CLI -> HTTP -> OIDC introspection double -> real Management handler ->
// PostgreSQL. No route stubs: command output, Idempotency-Key, RBAC, CAS,
// audit and delete-finalizer semantics are exercised together.
func TestManagementCtlRealPostgresOperatorLoop(t *testing.T) {
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
	for _, path := range []string{
		"../../internal/store/postgres/schema.sql",
		"../../internal/platform/management/schema.sql",
	} {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}

	suffix := fmt.Sprint(time.Now().UnixNano())
	cluster, project, pool, job := "pilotcluster-"+suffix, "pilotproject-"+suffix, "pilotpool-"+suffix, "pilotjob-"+suffix
	defer func() {
		for _, row := range []struct {
			query string
			id    string
		}{
			{"DELETE FROM management_operation_audit WHERE project_id=$1", project},
			{"DELETE FROM management_project_grants WHERE project_id=$1", project},
			{"DELETE FROM desired_resources WHERE cluster_id=$1", cluster},
			{"DELETE FROM cluster_bindings WHERE cluster_id=$1", cluster},
			{"DELETE FROM project_bindings WHERE project_id=$1", project},
		} {
			_, _ = db.ExecContext(ctx, row.query, row.id)
		}
	}()
	for _, row := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES ($1,$2,$3)", []any{project, cluster, project}},
		{"INSERT INTO cluster_bindings(pool_id,cluster_id,provider) VALUES ($1,$2,'kueue')", []any{pool, cluster}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES ($1,$2,$3,'operator')", []any{"https://issuer.example", "pilot-alice", project}},
	} {
		if _, err := db.ExecContext(ctx, row.query, row.args...); err != nil {
			t.Fatal(err)
		}
	}

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "pilot-client" || password != "pilot-secret" || r.Method != http.MethodPost {
			http.Error(w, "invalid client", http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("token") != "pilot-access-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true, "iss": "https://issuer.example",
			"sub": "pilot-alice", "aud": "pilot-api",
			"exp": time.Now().Add(time.Minute).Unix(), "token_type": "Bearer",
		})
	}))
	defer idp.Close()
	auth := management.Introspector{
		URL: idp.URL, Issuer: "https://issuer.example", Audience: "pilot-api",
		ClientID: "pilot-client", ClientSecret: "pilot-secret", AllowHTTPForTests: true,
	}
	handler, err := management.NewHandler(db, auth)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(handler)
	defer api.Close()
	t.Setenv("MANAGEMENT_ACCESS_TOKEN", "pilot-access-token")
	base := options{APIURL: api.URL, Cluster: cluster, Project: project}
	call := func(o options) (string, string, error) {
		t.Helper()
		var output, logs bytes.Buffer
		err := run(o, &output, &logs)
		if strings.Contains(output.String()+logs.String(), "pilot-access-token") {
			t.Fatal("CLI exposed bearer token")
		}
		return output.String(), logs.String(), err
	}

	poolJSON := `{"expectedGeneration":0,"spec":{"accelerators":[{"class":"h100","quota":2}],"acceleratorBindings":[{"class":"h100","resourceName":"nvidia.com/gpu","flavor":"h100"}],"scheduling":{"mode":"kueue"}}}`
	poolFile := filepath.Join(t.TempDir(), "pool.json")
	if err := os.WriteFile(poolFile, []byte(poolJSON), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := newOperationKey()
	if err != nil {
		t.Fatal(err)
	}
	poolPut := base
	poolPut.Action, poolPut.Kind, poolPut.Resource, poolPut.File, poolPut.OperationID = "put", "ComputePool", pool, poolFile, key
	if output, logs, err := call(poolPut); err != nil || !strings.Contains(output, "HTTP 204") || !strings.Contains(logs, key) {
		t.Fatalf("pool creation failed: output=%s logs=%s err=%v", output, logs, err)
	}
	poolRead := base
	poolRead.Action, poolRead.Kind, poolRead.Resource = "get", "ComputePool", pool
	if output, _, err := call(poolRead); err != nil || !strings.Contains(output, "HTTP 200") || !strings.Contains(output, `"generation":1`) {
		t.Fatalf("pool get failed: %s err=%v", output, err)
	}
	receipt := base
	receipt.Action, receipt.OperationID = "receipt", key
	if output, _, err := call(receipt); err != nil || !strings.Contains(output, "HTTP 200") || !strings.Contains(output, `"desired-committed"`) || !strings.Contains(output, key) {
		t.Fatalf("pool receipt not found: %s err=%v", output, err)
	}
	if output, _, err := call(poolPut); err != nil || !strings.Contains(output, "HTTP 204") {
		t.Fatalf("idempotent pool replay failed: %s err=%v", output, err)
	}
	changed := strings.Replace(poolJSON, `"quota":2`, `"quota":3`, 1)
	changedFile := filepath.Join(t.TempDir(), "changed.json")
	if err := os.WriteFile(changedFile, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	poolPut.File = changedFile
	if output, _, err := call(poolPut); err == nil || !strings.Contains(output, "HTTP 409") {
		t.Fatalf("reused operation ID did not fail closed: %s err=%v", output, err)
	}

	workloadJSON := fmt.Sprintf(`{"expectedGeneration":0,"poolId":%q,"spec":{"image":"example.invalid/pilot:1","accelerator":{"class":"h100","quota":1}}}`, pool)
	workFile := filepath.Join(t.TempDir(), "workload.json")
	if err := os.WriteFile(workFile, []byte(workloadJSON), 0600); err != nil {
		t.Fatal(err)
	}
	workKey, err := newOperationKey()
	if err != nil {
		t.Fatal(err)
	}
	workPut := base
	workPut.Action, workPut.Kind, workPut.Resource, workPut.File, workPut.OperationID = "put", "Workload", job, workFile, workKey
	if output, _, err := call(workPut); err != nil || !strings.Contains(output, "HTTP 204") {
		t.Fatalf("workload creation failed: %s err=%v", output, err)
	}

	poolDelete := base
	poolDelete.Action, poolDelete.Kind, poolDelete.Resource, poolDelete.Generation = "delete", "ComputePool", pool, 1
	poolDelete.OperationID, err = newOperationKey()
	if err != nil {
		t.Fatal(err)
	}
	if output, _, err := call(poolDelete); err == nil || !strings.Contains(output, "HTTP 409") {
		t.Fatalf("pool teardown bypassed existing Workload: %s err=%v", output, err)
	}
	workDelete := base
	workDelete.Action, workDelete.Kind, workDelete.Resource, workDelete.Generation = "delete", "Workload", job, 1
	workDelete.OperationID, err = newOperationKey()
	if err != nil {
		t.Fatal(err)
	}
	if output, _, err := call(workDelete); err != nil || !strings.Contains(output, "HTTP 202") {
		t.Fatalf("workload deletion request failed: %s err=%v", output, err)
	}
	if output, _, err := call(workDelete); err != nil || !strings.Contains(output, "HTTP 202") {
		t.Fatalf("delete replay failed: %s err=%v", output, err)
	}
	var deleting bool
	if err := db.QueryRowContext(ctx, "SELECT deletion_timestamp IS NOT NULL FROM desired_resources WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2", cluster, job).Scan(&deleting); err != nil || !deleting {
		t.Fatalf("workload finalizer deletion state=%t err=%v", deleting, err)
	}
	var auditCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM management_operation_audit WHERE project_id=$1", project).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("expected 3 atomic commit receipts, got=%d err=%v", auditCount, err)
	}

	// Live RBAC takes priority over old successful receipts and Desired.
	if _, err := db.ExecContext(ctx, "UPDATE management_project_grants SET role='viewer' WHERE issuer=$1 AND subject=$2 AND project_id=$3", "https://issuer.example", "pilot-alice", project); err != nil {
		t.Fatal(err)
	}
	if output, _, err := call(receipt); err == nil || !strings.Contains(output, "HTTP 404") {
		t.Fatalf("downgraded operator still read operation receipt: %s err=%v", output, err)
	}
	if output, _, err := call(workDelete); err == nil || !strings.Contains(output, "HTTP 403") {
		t.Fatalf("downgraded operator still mutated resource: %s err=%v", output, err)
	}
}
