package management

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// VerifyRuntimeDBPrivileges checks the effective PostgreSQL login authority
// before cmd/management-api starts accepting traffic. Do not use the schema
// owner or DBA connection string for the running management service.
//
// This is defense in depth, not a replacement for DBA role provisioning:
// privilege drift after startup must still be detected by database grants,
// separate monitoring and deployment admission.
func VerifyRuntimeDBPrivileges(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("management DB privilege check requires PostgreSQL")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("management DB privilege connection unavailable: %w", err)
	}
	defer conn.Close()

	if err := verifyRuntimeDBLoginIdentity(ctx, conn); err != nil {
		return err
	}

	// Check the actual login, including all effective PUBLIC, inherited and
	// column-level grants. A GRANT template alone cannot prove these.
	required := map[string]string{
		"management_project_grants":  "SELECT"
		"project_bindings":           "SELECT"
		"cluster_bindings":           "SELECT"
		"deletion_tombstones":        "SELECT"
		"desired_resources":          "SELECT, INSERT, UPDATE"
		"management_operation_audit": "SELECT, INSERT"
	}
	for table, privileges := range required {
		// PostgreSQL comma-lists test ANY right; require EACH explicitly.
		for _, right := range strings.Split(privileges, ",") {
			right = strings.TrimSpace(right)
			var ok bool
			err = conn.QueryRowContext(ctx, `
SELECT pg_catalog.has_table_privilege(current_user, $1::regclass, $2)
`, "public."+table, right).Scan(&ok)
			if err != nil || !ok {
				return fmt.Errorf("management DB missing required %s on %s", right, table)
			}
		}
	}

	// No column-specific grant may silently bypass a forbidden table-wide
	// grant check. has_any_column_privilege detects either form.
	forbidden := map[string]string{
		"management_project_grants":  "INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"project_bindings":           "INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"cluster_bindings":           "INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"deletion_tombstones":        "INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"desired_resources":          "DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"management_operation_audit": "UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"resource_observations":      "SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"reconcile_leases":           "SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"placement_migrations":       "SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
		"cluster_agents":             "SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER"
	}
	for table, privileges := range forbidden {
		var tableGrant bool
		err = conn.QueryRowContext(ctx, `
SELECT pg_catalog.has_table_privilege(current_user, $1::regclass, $2)
`, "public."+table, privileges).Scan(&tableGrant)
		if err != nil {
			return fmt.Errorf("management DB forbidden grant probe failed (%s): %w", table, err)
		}
		// Any_column_privilege includes legitimate SELECT or INSERT on
		// allowed tables; use per-right tests only on forbidden column rights.
		if tableGrant {
			return fmt.Errorf("management DB forbidden table authority on %s", table)
		}
		for _, right := range []string{"SELECT", "INSERT", "UPDATE", "REFERENCES"} {
			if strings.Contains(","+privileges+",", ","+right+",") {
				var present bool
				err = conn.QueryRowContext(ctx, `
SELECT pg_catalog.has_any_column_privilege(current_user, $1::regclass, $2)
`, "public."+table, right).Scan(&present)
				if err != nil || present {
					return fmt.Errorf("management DB forbidden column authority %s on %s", right, table)
				}
			}
		}
	}
	return nil
}

// runtimeLoginQuerier is satisfied by both a pinned live connection and a
// transaction; the latter is used to reject privileged SET ROLE sessions.
type runtimeLoginQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyRuntimeDBLoginIdentity(ctx context.Context, q runtimeLoginQuerier) error {
	var sessionUser, effectiveUser, schema string
	var superuser, createRole, createDB, bypassRLS, replication bool
	var canCreateSchema, canCreateDatabase bool
	err := q.QueryRowContext(ctx, `
SELECT session_user, current_user, current_schema(),
       r.rolsuper, r.rolcreaterole, r.rolcreatedb, r.rolbypassrls,
       r.rolreplication,
       has_schema_privilege(current_user, 'public', 'CREATE'),
       has_database_privilege(current_user, current_database(), 'CREATE')
FROM pg_catalog.pg_roles AS r WHERE r.rolname = current_user
`).Scan(&sessionUser, &effectiveUser, &schema,
		&superuser, &createRole, &createDB, &bypassRLS, &replication,
		&canCreateSchema, &canCreateDatabase)
	if err != nil {
		return fmt.Errorf("management DB login authority probe failed: %w", err)
	}
	if sessionUser != effectiveUser || schema != "public" {
		return errors.New("management DB must use its direct login in the public schema")
	}
	if superuser || createRole || createDB || bypassRLS || replication ||
		canCreateSchema || canCreateDatabase {
		return errors.New("management DB login has administrative or DDL authority")
	}
	return nil
}
