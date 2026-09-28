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

CREATE INDEX IF NOT EXISTS desired_resources_cluster_generation_idx
    ON desired_resources (cluster_id, generation);

CREATE INDEX IF NOT EXISTS resource_observations_cluster_generation_idx
    ON resource_observations (cluster_id, observed_generation);
