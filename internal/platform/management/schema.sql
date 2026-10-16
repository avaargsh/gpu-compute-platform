-- Management-plane role grants are provisioned by trusted DBA automation.
CREATE TABLE IF NOT EXISTS management_project_grants (
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 project_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK (role IN ('viewer', 'operator', 'admin')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (issuer, subject, project_id)
);

-- The audit row is inserted in the same transaction as the Desired write.
-- Runtime DB grants: SELECT + INSERT only; never UPDATE or DELETE.
CREATE TABLE IF NOT EXISTS management_operation_audit (
 request_id TEXT PRIMARY KEY,
 request_digest TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 project_id TEXT NOT NULL,
 cluster_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('ComputePool', 'Workload')),
 resource_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK (action IN ('create', 'update', 'delete-request')),
 expected_generation BIGINT NOT NULL,
 resulting_generation BIGINT NOT NULL,
 prior_spec_sha256 TEXT NOT NULL,
 resulting_spec_sha256 TEXT NOT NULL
);

-- Additive migration for pilot databases that already held audit receipts.
-- Older records have no client idempotency fingerprint, so cannot replay.
ALTER TABLE management_operation_audit
 ADD COLUMN IF NOT EXISTS request_digest TEXT;
UPDATE management_operation_audit
 SET request_digest = repeat('0', 64) WHERE request_digest IS NULL;
ALTER TABLE management_operation_audit
 ALTER COLUMN request_digest SET NOT NULL;

CREATE INDEX IF NOT EXISTS management_audit_project_time_idx
 ON management_operation_audit (project_id, created_at DESC);

-- Subject-owned keyset discovery uses issuer+subject+project+cluster equality
-- before the timestamp/ID sort. LIMIT 26 alone cannot bound database work.
-- The time column is the audit transaction-start timestamp, not commit order.
CREATE INDEX IF NOT EXISTS management_audit_owner_recent_idx
 ON management_operation_audit
 (issuer, subject, project_id, cluster_id, created_at DESC, request_id DESC);


-- Read-only Management API login must not receive UPDATE on authorization
-- tables. PostgreSQL SELECT ... FOR SHARE does require UPDATE privilege, so
-- a narrowly scoped DBA-owned SECURITY DEFINER function takes the row locks.
-- Only fixed identity and binding parameters are accepted: no dynamic SQL.
-- pg_temp is excluded from object resolution and every table is qualified.
CREATE OR REPLACE FUNCTION public.management_locked_project_role(
  p_issuer text, p_subject text, p_project_id text, p_cluster_id text
) RETURNS SETOF text
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $management_role$
  SELECT g.role
  FROM public.management_project_grants AS g
  JOIN public.project_bindings AS b
    ON b.project_id = g.project_id AND b.cluster_id = p_cluster_id
  WHERE g.issuer = p_issuer AND g.subject = p_subject
    AND g.project_id = p_project_id
  FOR SHARE OF g, b;
$management_role$;

-- Functions are executable by PUBLIC by default: explicitly revoke this
-- narrow privileged function, then grant only to Management's runtime role.
REVOKE ALL ON FUNCTION public.management_locked_project_role(
  text, text, text, text
) FROM PUBLIC;

-- Both ComputePool creation and Workload placement need the CURRENT pool
-- binding locked through their Desired/audit commit. Direct FOR SHARE on
-- cluster_bindings requires UPDATE and would violate runtime least privilege.
-- The DBA owns this non-dynamic, schema-qualified function and exposes
-- EXECUTE only to the Management runtime role.
CREATE OR REPLACE FUNCTION public.management_locked_pool_binding(
  p_pool_id text
) RETURNS TABLE(cluster_id text, provider text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $management_pool_binding$
  SELECT b.cluster_id, b.provider
  FROM public.cluster_bindings AS b
  WHERE b.pool_id = p_pool_id
  FOR SHARE OF b;
$management_pool_binding$;

REVOKE ALL ON FUNCTION public.management_locked_pool_binding(text) FROM PUBLIC;


-- Read-only, project-scoped execution observation projection. Management's
-- runtime login MUST NOT have direct SELECT on resource_observations.
-- The DBA owns this static SQL function and grants EXECUTE only to the
-- narrowly provisioned Management runtime role. The caller cannot supply a
-- role or a different Project for an observation whose Desired belongs to
-- another tenant. This read is a snapshot, not an execution attestation.
CREATE OR REPLACE FUNCTION public.management_scoped_workload_observation(
  p_issuer text, p_subject text, p_cluster text,
  p_project text, p_resource text
) RETURNS TABLE (
  desired_generation bigint,
  deleting boolean,
  observed_generation bigint,
  conditions jsonb,
  observed_at timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $management_observation$
  SELECT d.generation, (d.deletion_timestamp IS NOT NULL),
         o.observed_generation, o.conditions, o.observed_at
  FROM public.desired_resources AS d
  JOIN public.project_bindings AS b
    ON b.project_id=p_project AND b.cluster_id=d.cluster_id
  JOIN public.management_project_grants AS g
    ON g.project_id=b.project_id AND g.issuer=p_issuer
   AND g.subject=p_subject AND g.role IN ('viewer','operator','admin')
  LEFT JOIN public.resource_observations AS o
    ON o.cluster_id=d.cluster_id AND o.kind=d.kind
   AND o.resource_id=d.resource_id
  WHERE d.cluster_id=p_cluster AND d.kind='Workload'
    AND d.resource_id=p_resource AND d.spec->>'projectID'=p_project;
$management_observation$;

REVOKE ALL ON FUNCTION public.management_scoped_workload_observation(
  text,text,text,text,text
) FROM PUBLIC;
