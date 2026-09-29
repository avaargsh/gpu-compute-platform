CREATE TABLE IF NOT EXISTS cluster_agents (
    cluster_id TEXT PRIMARY KEY,
    agent_version TEXT NOT NULL,
    kubernetes_version TEXT NOT NULL DEFAULT '',
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS desired_resources (
    cluster_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    spec JSONB NOT NULL,
    deletion_timestamp TIMESTAMPTZ,
    finalizers JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, kind, resource_id)
);

CREATE TABLE IF NOT EXISTS resource_observations (
    cluster_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    observed_generation BIGINT NOT NULL,
    conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, kind, resource_id)
);

ALTER TABLE desired_resources
    ADD COLUMN IF NOT EXISTS deletion_timestamp TIMESTAMPTZ;

ALTER TABLE desired_resources
    ADD COLUMN IF NOT EXISTS finalizers JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS desired_resources_cluster_generation_idx
    ON desired_resources (cluster_id, generation);

CREATE INDEX IF NOT EXISTS resource_observations_cluster_generation_idx
    ON resource_observations (cluster_id, observed_generation);


CREATE TABLE IF NOT EXISTS project_bindings (
    project_id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL,
    namespace TEXT NOT NULL,
    generation BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cluster_bindings (
    pool_id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    generation BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS project_bindings_cluster_idx
    ON project_bindings (cluster_id);

CREATE INDEX IF NOT EXISTS cluster_bindings_cluster_idx
    ON cluster_bindings (cluster_id);


CREATE TABLE IF NOT EXISTS placement_migrations (
    migration_id TEXT PRIMARY KEY,
    pool_id TEXT NOT NULL,
    source_cluster_id TEXT NOT NULL,
    target_cluster_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    phase TEXT NOT NULL,
    conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE placement_migrations
    ADD COLUMN IF NOT EXISTS source_generation BIGINT NOT NULL DEFAULT 0;

ALTER TABLE placement_migrations
    ADD COLUMN IF NOT EXISTS target_generation BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS placement_migrations_pool_idx
    ON placement_migrations (pool_id, created_at);

CREATE TABLE IF NOT EXISTS reconcile_leases (
    cluster_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    owner TEXT NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    epoch BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, kind, resource_id)
);

ALTER TABLE reconcile_leases
    ADD COLUMN IF NOT EXISTS epoch BIGINT NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS reconcile_leases_expiry_idx
    ON reconcile_leases (lease_until);


CREATE TABLE IF NOT EXISTS deletion_tombstones (
    cluster_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    finalized_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, kind, resource_id)
);

CREATE INDEX IF NOT EXISTS deletion_tombstones_finalized_idx
    ON deletion_tombstones (finalized_at);
