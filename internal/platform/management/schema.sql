-- Deploy with the management API on a dedicated PostgreSQL role.
-- A grant is provisioned by trusted DBA automation, never by browser users.
CREATE TABLE IF NOT EXISTS management_project_grants (
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 project_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK (role IN ('viewer', 'operator', 'admin')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (issuer, subject, project_id)
);

-- Audit and desired resource mutation MUST commit in one DB transaction.
-- Audit inserts are append-only for the management runtime DB role; grant
-- SELECT/INSERT but never UPDATE/DELETE/TRUNCATE on this table.
CREATE TABLE IF NOT EXISTS management_operation_audit (
 request_id TEXT PRIMARY KEY,
 request_digest TEXT NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64} created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 project_id TEXT NOT NULL,
 cluster_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('ComputePool', 'Workload')),
 resource_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK (action IN ('create','update','delete-request')),
 expected_generation BIGINT NOT NULL,
 resulting_generation BIGINT NOT NULL,
 prior_spec_sha256 TEXT NOT NULL,
 resulting_spec_sha256 TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS management_audit_project_time_idx
 ON management_operation_audit (project_id, created_at DESC);
),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 project_id TEXT NOT NULL,
 cluster_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('ComputePool', 'Workload')),
 resource_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK (action IN ('create','update','delete-request')),
 expected_generation BIGINT NOT NULL,
 resulting_generation BIGINT NOT NULL,
 prior_spec_sha256 TEXT NOT NULL,
 resulting_spec_sha256 TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS management_audit_project_time_idx
 ON management_operation_audit (project_id, created_at DESC);
