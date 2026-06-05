-- Ensure pgcrypto is available for gen_random_bytes (built-in on PG 13+, extension on older).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Part 1: Add stable route_key column to nodes for CloudFront origin path routing.

ALTER TABLE nodes ADD COLUMN route_key TEXT;

UPDATE nodes SET route_key = encode(gen_random_bytes(8), 'hex') WHERE route_key IS NULL;

ALTER TABLE nodes ALTER COLUMN route_key SET NOT NULL;
ALTER TABLE nodes ADD CONSTRAINT uq_nodes_route_key UNIQUE (route_key);

-- Part 2: Singleton table for CloudFront subscription distribution.

CREATE TABLE cloudfront_configs (
    id                       TEXT PRIMARY KEY DEFAULT 'cf-global',
    access_key_id            TEXT        NOT NULL DEFAULT '',
    encrypted_secret_key     TEXT        NOT NULL DEFAULT '',
    encrypted_session_token  TEXT        NOT NULL DEFAULT '',
    region                   TEXT        NOT NULL DEFAULT 'us-east-1',
    origins_json             TEXT        NOT NULL DEFAULT '[]',
    distribution_id          TEXT        NOT NULL DEFAULT '',
    distribution_domain      TEXT        NOT NULL DEFAULT '',
    distribution_mode        TEXT        NOT NULL DEFAULT 'managed',
    bindings_json            TEXT        NOT NULL DEFAULT '[]',
    plan_json                TEXT        NOT NULL DEFAULT '[]',
    last_synced_at           TIMESTAMPTZ,
    sync_status              TEXT        NOT NULL DEFAULT 'idle',
    last_sync_error          TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
