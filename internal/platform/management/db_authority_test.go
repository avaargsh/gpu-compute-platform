package management

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
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
	accept := func(label string) {
		t.Helper()
		if err := VerifyRuntimeDBPrivileges(ctx, runtimeDB); err != nil {
			t.Fatalf("%s: legitimate minimal-privilege LOGIN rejected: %v", label, err)
		}
	}
	reject := func(label string) {
		t.Helper()
		if err := VerifyRuntimeDBPrivileges(ctx, runtimeDB); err == nil {
			t.Fatalf("%s: overprivileged LOGIN accepted", label)
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
