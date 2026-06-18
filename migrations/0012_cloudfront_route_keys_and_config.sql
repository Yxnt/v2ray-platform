-- Ensure pgcrypto is available for gen_random_bytes (built-in on PG 13+, extension on older).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Part 1: Add stable route_key column to nodes for CloudFront origin path routing.

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS route_key TEXT;

UPDATE nodes SET route_key = encode(gen_random_bytes(8), 'hex') WHERE route_key IS NULL;

ALTER TABLE nodes ALTER COLUMN route_key SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'uq_nodes_route_key'
          AND conrelid = 'nodes'::regclass
    ) THEN
        ALTER TABLE nodes ADD CONSTRAINT uq_nodes_route_key UNIQUE (route_key);
    END IF;
END $$;

-- Part 2: Singleton table for CloudFront subscription distribution.

CREATE TABLE IF NOT EXISTS cloudfront_configs (
    id                          TEXT PRIMARY KEY DEFAULT 'cf-global',
    encrypted_access_key_id     TEXT        NOT NULL DEFAULT '',
    encrypted_secret_access_key TEXT        NOT NULL DEFAULT '',
    encrypted_session_token     TEXT        NOT NULL DEFAULT '',
    aws_region                  TEXT        NOT NULL DEFAULT 'us-east-1',
    enabled                     BOOLEAN     NOT NULL DEFAULT FALSE,
    custom_entry_host           TEXT        NOT NULL DEFAULT '',
    mode                        TEXT        NOT NULL DEFAULT 'managed',
    distribution_id             TEXT        NOT NULL DEFAULT '',
    distribution_domain_name    TEXT        NOT NULL DEFAULT '',
    origins_json                TEXT        NOT NULL DEFAULT '[]',
    bindings_json               TEXT        NOT NULL DEFAULT '[]',
    plan_json                   TEXT        NOT NULL DEFAULT '[]',
    last_synced_at              TIMESTAMPTZ,
    last_successful_sync_at     TIMESTAMPTZ,
    sync_status                 TEXT        NOT NULL DEFAULT 'idle',
    drift_status                TEXT        NOT NULL DEFAULT '',
    last_sync_error             TEXT        NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);
