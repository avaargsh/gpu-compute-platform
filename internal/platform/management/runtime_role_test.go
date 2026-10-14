package management

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// This test uses a disposable, uniquely named NOLOGIN PostgreSQL role.
// Never run it against a production DSN. It requires the opt-in test fixture.
func TestManagementRuntimeRoleLeastPrivilegeWithPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires TEST_POSTGRES_DSN disposable PostgreSQL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var canCreateRole bool
	if err := db.QueryRowContext(ctx, "SELECT rolsuper OR rolcreaterole FROM pg_roles WHERE rolname=current_user").Scan(&canCreateRole); err != nil {
		t.Fatal(err)
	}
	if !canCreateRole {
		t.Skip("least-privilege integration requires a disposable test DB role with CREATEROLE")
	}
	for _, path := range []string{"../../store/postgres/schema.sql", "schema.sql"} {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}

	role := fmt.Sprintf("gpu_management_acl_test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE ROLE "+role+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// DROP OWNED revokes test-role grants; the generated role owns no data.
		if _, err := db.ExecContext(context.Background(), "DROP OWNED BY "+role); err != nil {
			t.Errorf("revoke test role: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), "DROP ROLE "+role); err != nil {
			t.Errorf("drop test role: %v", err)
		}
	}()
	grantSQL, err := os.ReadFile("runtime_role.sql")
	if err != nil {
		t.Fatal(err)
	}
	// The replacement is only in this test: role is generated from a fixed
	// identifier prefix and UnixNano, never from user-controlled input.
	grant := strings.ReplaceAll(string(grantSQL), "gpu_management_runtime", role)
	if _, err := db.ExecContext(ctx, grant); err != nil {
		t.Fatalf("runtime grants cannot be applied by test DBA: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// DBA seeds an authorization row inside the never-committed test tx.
	// The least-privilege runtime role cannot create or modify these rows.
	project := fmt.Sprintf("role-test-project-%d", time.Now().UnixNano())
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO public.project_bindings(project_id,cluster_id,namespace) VALUES($1,'role-test-cluster','role-test')", project); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO public.management_project_grants(issuer,subject,project_id,role) VALUES('test-issuer','test-subject',$1,'operator')", project); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE "+role); err != nil {
		t.Fatal(err)
	}
	var lockedRole string
	if err := tx.QueryRowContext(ctx,
		"SELECT role FROM public.management_locked_project_role('test-issuer','test-subject',$1,'role-test-cluster') AS locked(role)", project).Scan(&lockedRole); err != nil || lockedRole != "operator" {
		t.Fatalf("DBA-owned fixed query must lock and return current role, role=%q err=%v", lockedRole, err)
	}
	var effective string
	if err := tx.QueryRowContext(ctx, "SELECT current_user").Scan(&effective); err != nil || effective != role {
		t.Fatalf("runtime role not effective: user=%q err=%v", effective, err)
	}

	// Positive runtime capabilities: only the SQL used for management
	// authorization, Desired CAS, and append-only audit.
	for _, query := range []string{
		"SELECT 1 FROM public.management_project_grants LIMIT 1",
		"SELECT 1 FROM public.project_bindings LIMIT 1",
		"SELECT 1 FROM public.cluster_bindings LIMIT 1",
		"SELECT 1 FROM public.deletion_tombstones LIMIT 1",
		"SELECT 1 FROM public.desired_resources LIMIT 1",
		"SELECT 1 FROM public.management_operation_audit LIMIT 1",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatalf("required runtime read unavailable (%s): %v", query, err)
		}
	}
	tag := fmt.Sprintf("runtime-acl-%d", time.Now().UnixNano())
	if _, err := tx.ExecContext(ctx, "INSERT INTO public.desired_resources(cluster_id,kind,resource_id,generation,spec) VALUES($1,'ComputePool',$2,1,$3::jsonb)", tag, tag, "{}"); err != nil {
		t.Fatalf("Desired INSERT unavailable: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE public.desired_resources SET generation=2 WHERE cluster_id=$1 AND kind='ComputePool' AND resource_id=$1", tag); err != nil {
		t.Fatalf("Desired CAS UPDATE unavailable: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.management_operation_audit
(request_id,request_digest,issuer,subject,project_id,cluster_id,kind,resource_id,
 action,expected_generation,resulting_generation,prior_spec_sha256,resulting_spec_sha256)
VALUES ($1,$2,'test-issuer','test-subject','project',$3,'ComputePool',$3,'create',0,1,$2,$2)`, tag, strings.Repeat("a", 64), tag); err != nil {
		t.Fatalf("append-only audit INSERT unavailable: %v", err)
	}

	// Every probe runs in its own savepoint so a denied operation cannot
	// abort the transaction and hide subsequent checks.
	for _, query := range []string{
		// The direct row-lock requires UPDATE and remains prohibited.
		"SELECT role FROM public.management_project_grants FOR SHARE",
		"UPDATE public.management_operation_audit SET action='update' WHERE false",
		"DELETE FROM public.management_operation_audit WHERE false",
		"UPDATE public.management_project_grants SET role='admin' WHERE false",
		"DELETE FROM public.management_project_grants WHERE false",
		"UPDATE public.project_bindings SET namespace='admin' WHERE false",
		"UPDATE public.cluster_bindings SET provider='other' WHERE false",
		"DELETE FROM public.desired_resources WHERE false",
		"SELECT 1 FROM public.resource_observations LIMIT 1",
		"SELECT 1 FROM public.cluster_agents LIMIT 1",
		"CREATE TABLE public.management_runtime_must_not_create(id integer)",
	} {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT acl_probe"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.ExecContext(ctx, query)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("unauthorized statement not denied with 42501 (%s): %v", query, err)
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT acl_probe"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT acl_probe"); err != nil {
			t.Fatal(err)
		}
	}
	// Rollback removes the positive insert probes; this test never modifies
	// the real Desired/audit state outside its own transaction.
}
