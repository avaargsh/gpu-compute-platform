package management

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// A fresh LOGIN is essential: SET LOCAL ROLE on a DBA connection would test
// the effective role but NOT the unrestricted session_user behind it.
func TestManagementActualLoginAuthorityAdmission(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable TEST_POSTGRES_DSN")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var authorized bool
	if err := admin.QueryRowContext(ctx,
		"SELECT rolsuper OR rolcreaterole FROM pg_roles WHERE rolname=current_user",
	).Scan(&authorized); err != nil {
		t.Fatal(err)
	}
	if !authorized {
		t.Skip("requires disposable test admin with CREATEROLE")
	}
	for _, path := range []string{"../../store/postgres/schema.sql", "schema.sql"} {
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.ExecContext(ctx, string(src)); err != nil {
			t.Fatal(err)
		}
	}
	prefix := fmt.Sprintf("mgmt_guard_%d", time.Now().UnixNano())
	group, login := prefix+"_group", prefix+"_login"
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		t.Fatal(err)
	}
	password := hex.EncodeToString(passwordBytes) // SQL literal safe (hex only)
	if _, err := admin.ExecContext(ctx, "CREATE ROLE "+group+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// All identifiers are constructed here, never supplied by clients.
		_, _ = admin.ExecContext(context.Background(), "REVOKE "+group+" FROM "+login)
		_, _ = admin.ExecContext(context.Background(), "DROP OWNED BY "+login)
		_, _ = admin.ExecContext(context.Background(), "DROP OWNED BY "+group)
		if _, err := admin.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+login); err != nil {
			t.Errorf("drop disposable login: %v", err)
		}
		if _, err := admin.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+group); err != nil {
			t.Errorf("drop disposable group: %v", err)
		}
	}()
	if _, err := admin.ExecContext(ctx, "CREATE ROLE "+login+" LOGIN INHERIT PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	grantSrc, err := os.ReadFile("runtime_role.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, strings.ReplaceAll(
		string(grantSrc), "gpu_management_runtime", group,
	)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "GRANT "+group+" TO "+login); err != nil {
		t.Fatal(err)
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.User, cfg.Password = login, password
	// Test the actual effective session and its inherited privileges on
	// a newly authenticated connection, not a simulated SET ROLE.
	runtimeDB := stdlib.OpenDB(*cfg)
	defer runtimeDB.Close()
	if err := runtimeDB.PingContext(ctx); err != nil {
		t.Fatalf("cannot connect as disposable management LOGIN: %v", err)
	}
	// Exercise the exact guarded mutation entry used by the production
	// handler. No RBAC grant exists for this synthetic identity, so an
	// authorized DB login reaches 403; unsafe DB privileges stop earlier
	// with 503. This test never performs a Desired/audit mutation.
	mutationStatus := func() int {
		zero := int64(0)
		svc := &Service{DB: runtimeDB, RequireRuntimeDBAuthority: true}
		status, _ := svc.mutate(ctx, Identity{Issuer: "test-issuer", Subject: "no-grant"},
			"cluster", "project", "ComputePool", "dryrun",
			mutationInput{ExpectedGeneration: &zero}, http.MethodPut,
			"00000000000000000000000000000000")
		return status
	}
	accept := func(label string) {
		t.Helper()
		if err := VerifyRuntimeDBPrivileges(ctx, runtimeDB); err != nil {
			t.Fatalf("%s: legitimate minimal-privilege LOGIN rejected: %v", label, err)
		}
		if status := mutationStatus(); status != http.StatusForbidden {
			t.Fatalf("%s: safe LOGIN should reach project RBAC (403), got %d", label, status)
		}
	}
	reject := func(label string) {
		t.Helper()
		if err := VerifyRuntimeDBPrivileges(ctx, runtimeDB); err == nil {
			t.Fatalf("%s: overprivileged LOGIN accepted", label)
		}
		if status := mutationStatus(); status != http.StatusServiceUnavailable {
			t.Fatalf("%s: guarded mutation did not stop on DB drift (503), got %d", label, status)
		}
	}
	revoke := func(grant, undo, label string) {
		t.Helper()
		if _, err := admin.ExecContext(ctx, grant); err != nil {
			t.Fatalf("%s: granting privilege: %v", label, err)
		}
		reject(label)
		if _, err := admin.ExecContext(ctx, undo); err != nil {
			t.Fatalf("%s: revoke privilege: %v", label, err)
		}
		accept(label + " revoked")
	}

	accept("legitimate inherited narrow group grants")
	// Positive product golden path: the REAL narrow LOGIN must complete
	// a ComputePool create/update and audit through the guarded HTTP handler.
	// A fail-closed negative-only test can pass even if valid users get 503.
	cluster, project, pool := prefix+"_cluster", prefix+"_project", prefix+"_pool"
	defer func() {
		for _, item := range []struct {
			query string
			arg   string
		}{
			{"DELETE FROM public.management_operation_audit WHERE project_id=$1", project},
			{"DELETE FROM public.desired_resources WHERE cluster_id=$1", cluster},
			{"DELETE FROM public.management_project_grants WHERE project_id=$1", project},
			{"DELETE FROM public.cluster_bindings WHERE cluster_id=$1", cluster},
			{"DELETE FROM public.project_bindings WHERE project_id=$1", project},
		} {
			if _, err := admin.ExecContext(context.Background(), item.query, item.arg); err != nil {
				t.Errorf("clean up narrow-login E2E: %v", err)
			}
		}
	}()
	for _, item := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO public.project_bindings(project_id,cluster_id,namespace) VALUES ($1,$2,$3)", []any{project, cluster, project}},
		{"INSERT INTO public.cluster_bindings(pool_id,cluster_id,provider) VALUES ($1,$2,'kueue')", []any{pool, cluster}},
		{"INSERT INTO public.management_project_grants(issuer,subject,project_id,role) VALUES ('https://issuer.example','alice',$1,'operator')", []any{project}},
	} {
		if _, err := admin.ExecContext(ctx, item.query, item.args...); err != nil {
			t.Fatal(err)
		}
	}
	idpAuth, idpServer := oidcFixture(t)
	defer idpServer.Close()
	handler, err := NewGuardedHandlerWithBrowserOrigin(runtimeDB, idpAuth, "")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/v1/management/clusters/" + cluster + "/projects/" + project + "/resources/ComputePool/" + pool
	put := func(body, key string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, endpoint, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer good-access-token")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		return resp.Code
	}
	poolSpec := func(expected, quota int) string {
		return fmt.Sprintf(`{"expectedGeneration":%d,"spec":{"accelerators":[{"class":"h100","quota":%d}],"acceleratorBindings":[{"class":"h100","resourceName":"nvidia.com/gpu","flavor":"h100"}],"scheduling":{"mode":"kueue"}}}`, expected, quota)
	}
	createID, updateID := "11111111111111111111111111111111", "22222222222222222222222222222222"
	if got := put(poolSpec(0, 2), createID); got != http.StatusNoContent {
		t.Fatalf("least-privilege production handler create returned %d, want 204", got)
	}
	if got := put(poolSpec(0, 2), createID); got != http.StatusNoContent {
		t.Fatalf("least-privilege idempotent create replay returned %d, want 204", got)
	}
	// A new DBA UPDATE grant should immediately block both a new mutation
	// and its requested receipt; denied writes cannot reach the audit table.
	if _, err := admin.ExecContext(ctx, "GRANT UPDATE ON public.management_operation_audit TO "+login); err != nil {
		t.Fatal(err)
	}
	if got := put(poolSpec(1, 3), updateID); got != http.StatusServiceUnavailable {
		t.Fatalf("runtime authority drift mutation returned %d, want 503", got)
	}
	var generation int64
	var auditCount int
	readCommitted := func() {
		t.Helper()
		if err := admin.QueryRowContext(ctx,
			"SELECT generation FROM public.desired_resources WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$2",
			cluster, pool).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRowContext(ctx,
			"SELECT count(*) FROM public.management_operation_audit WHERE project_id=$1", project).Scan(&auditCount); err != nil {
			t.Fatal(err)
		}
	}
	readCommitted()
	if generation != 1 || auditCount != 1 {
		t.Fatalf("denied drift mutation changed Desired/audit: gen=%d audit=%d", generation, auditCount)
	}
	if _, err := admin.ExecContext(ctx, "REVOKE UPDATE ON public.management_operation_audit FROM "+login); err != nil {
		t.Fatal(err)
	}
	if got := put(poolSpec(1, 3), updateID); got != http.StatusNoContent {
		t.Fatalf("revoked authority drift did not recover valid update: %d", got)
	}
	readCommitted()
	if generation != 2 || auditCount != 2 {
		t.Fatalf("legitimate update missing/duplicated: gen=%d audit=%d", generation, auditCount)
	}
	// Revoke the exact DBA-owned lock function while retaining read-only
	// table grants. A production write must fail BEFORE any transaction.
	const lockFunction = "public.management_locked_project_role(text,text,text,text)"
	if _, err := admin.ExecContext(ctx, "REVOKE EXECUTE ON FUNCTION "+lockFunction+" FROM "+group); err != nil {
		t.Fatal(err)
	}
	reject("row-lock function EXECUTE revoked")
	if _, err := admin.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION "+lockFunction+" TO "+group); err != nil {
		t.Fatal(err)
	}
	accept("row-lock function EXECUTE restored")
	// PostgreSQL >=16 can grant SET TRUE and INHERIT FALSE independently.
	// This secondary role is not effective in the LOGIN's current session,
	// but the LOGIN can still SET ROLE to it and mutate grants or audit.
	// A check limited to has_table_privilege(current_user, ...) misses it.
	setOnly := prefix + "_setonly"
	if _, err := admin.ExecContext(ctx, "CREATE ROLE "+setOnly+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "REVOKE "+setOnly+" FROM "+login)
		_, _ = admin.ExecContext(context.Background(), "DROP OWNED BY "+setOnly)
		if _, err := admin.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+setOnly); err != nil {
			t.Errorf("drop SET-only test role: %v", err)
		}
	}()
	if _, err := admin.ExecContext(ctx, "GRANT UPDATE ON public.management_project_grants TO "+setOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "GRANT "+setOnly+" TO "+login+" WITH INHERIT FALSE, SET TRUE"); err != nil {
		t.Fatal(err)
	}
	reject("noninherited SET-able role with RBAC mutation authority")
	if _, err := admin.ExecContext(ctx, "REVOKE "+setOnly+" FROM "+login); err != nil {
		t.Fatal(err)
	}
	accept("SET-able privilege escalation removed")
	// Role attributes such as CREATEDB do not have to be inherited to
	// become available after SET ROLE. Even an inherited group must not
	// carry administrator capabilities.
	if _, err := admin.ExecContext(ctx, "ALTER ROLE "+group+" CREATEDB"); err != nil {
		t.Fatal(err)
	}
	reject("inherited SET-able group with elevated administrative attribute")
	if _, err := admin.ExecContext(ctx, "ALTER ROLE "+group+" NOCREATEDB"); err != nil {
		t.Fatal(err)
	}
	accept("administrative group attribute removed")
	if err := VerifyRuntimeDBPrivileges(ctx, admin); err == nil {
		t.Fatal("a connected DBA/superuser must fail the runtime admission")
	}
	revoke(
		"GRANT UPDATE ON public.management_operation_audit TO "+login,
		"REVOKE UPDATE ON public.management_operation_audit FROM "+login,
		"mutable audit table",
	)
	revoke(
		"GRANT UPDATE(role) ON public.management_project_grants TO "+login,
		"REVOKE UPDATE(role) ON public.management_project_grants FROM "+login,
		"column-scoped RBAC mutation",
	)
	revoke(
		"GRANT SELECT ON public.cluster_agents TO "+login,
		"REVOKE SELECT ON public.cluster_agents FROM "+login,
		"cross-plane Agent metadata access",
	)
	revoke(
		"GRANT DELETE ON public.desired_resources TO "+login,
		"REVOKE DELETE ON public.desired_resources FROM "+login,
		"hard delete bypasses provider finalizer",
	)
	revoke(
		"GRANT CREATE ON SCHEMA public TO "+login,
		"REVOKE CREATE ON SCHEMA public FROM "+login,
		"schema object injection",
	)
	revoke(
		"ALTER ROLE "+login+" CREATEDB",
		"ALTER ROLE "+login+" NOCREATEDB",
		"database creation authority",
	)
	// Even with a safe effective role, a DBA session that can SET ROLE
	// back to a privileged principal must never pass admission.
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE "+group); err != nil {
		t.Fatal(err)
	}
	// Use a transaction-backed query adapter directly so the test checks
	// that session_user differs from current_user.
	if err := verifyRuntimeDBLoginIdentity(ctx, tx); err == nil {
		t.Fatal("SET ROLE DBA session unexpectedly accepted")
	}
}
