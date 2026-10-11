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
