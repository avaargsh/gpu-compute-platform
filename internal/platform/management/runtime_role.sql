-- Run as a trusted DBA *after* both PostgreSQL schemas are migrated.
-- The runtime role MUST already exist and MUST NOT own any database objects.
-- Example: CREATE ROLE gpu_management_runtime NOLOGIN; then attach a separately
-- provisioned LOGIN principal with a secret kept outside source control.
-- Substitute this role name under DBA change control if required.
--
-- This SQL grants only the permissions needed by cmd/management-api:
-- management authz and binding tables: SELECT only
-- Desired: SELECT, INSERT, UPDATE (CAS/finalizers; never DELETE)
-- audit: SELECT, INSERT (immutable from the application's viewpoint)
-- NO rights on observations, leases, placement migrations or agent metadata.
--
-- Do NOT give the application's LOGIN principal another path to table owner,
-- superuser, BYPASSRLS or a broad inherited role. Revoke extraneous rights
-- through DBA automation and verify effective privileges before rollout.
-- This template does NOT enforce network/TLS, per-tenant DB RLS or Agent mTLS.
BEGIN;

REVOKE ALL PRIVILEGES ON TABLE
  public.management_project_grants,
  public.management_operation_audit,
  public.project_bindings,
  public.cluster_bindings,
  public.desired_resources,
  public.deletion_tombstones,
  public.resource_observations,
  public.reconcile_leases,
  public.placement_migrations,
  public.cluster_agents
FROM gpu_management_runtime;

GRANT USAGE ON SCHEMA public TO gpu_management_runtime;

GRANT SELECT ON TABLE
  public.management_project_grants,
  public.project_bindings,
  public.cluster_bindings,
  public.deletion_tombstones
TO gpu_management_runtime;

GRANT SELECT, INSERT, UPDATE ON TABLE public.desired_resources
TO gpu_management_runtime;

GRANT SELECT, INSERT ON TABLE public.management_operation_audit
TO gpu_management_runtime;

-- The only elevated operation is an owned, fixed-query authorization read
-- with a row lock. Do not grant UPDATE on either authorization table.
REVOKE ALL ON FUNCTION public.management_locked_project_role(
  text, text, text, text
) FROM gpu_management_runtime;
GRANT EXECUTE ON FUNCTION public.management_locked_project_role(
  text, text, text, text
) TO gpu_management_runtime;

COMMIT;
