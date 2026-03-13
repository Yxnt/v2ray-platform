CREATE TABLE admins (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE members (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE bootstrap_tokens (
    id TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    public_host TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    runtime_flavor TEXT NOT NULL DEFAULT 'v2ray',
    status TEXT NOT NULL DEFAULT 'provisioning',
    last_heartbeat_at TIMESTAMPTZ,
    current_config_version BIGINT NOT NULL DEFAULT 0,
    node_token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE member_access_grants (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (node_id, member_id)
);

CREATE TABLE node_credentials (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    member_id TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    access_grant_id TEXT NOT NULL REFERENCES member_access_grants(id) ON DELETE CASCADE,
    credential_uuid UUID NOT NULL UNIQUE,
    email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE node_config_revisions (
    id BIGSERIAL PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    config_version BIGINT NOT NULL,
    config_json JSONB NOT NULL,
    config_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (node_id, config_version)
);

CREATE TABLE node_sync_events (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    config_version BIGINT NOT NULL,
    success BOOLEAN NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE usage_snapshots (
    id BIGSERIAL PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    member_id TEXT REFERENCES members(id) ON DELETE SET NULL,
    credential_uuid UUID,
    uplink_bytes BIGINT NOT NULL DEFAULT 0,
    downlink_bytes BIGINT NOT NULL DEFAULT 0,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_admin_id TEXT REFERENCES admins(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_nodes_status ON nodes(status);
CREATE INDEX idx_nodes_last_heartbeat_at ON nodes(last_heartbeat_at);
CREATE INDEX idx_member_access_grants_node_id ON member_access_grants(node_id);
CREATE INDEX idx_member_access_grants_member_id ON member_access_grants(member_id);
CREATE INDEX idx_node_credentials_node_id ON node_credentials(node_id);
CREATE INDEX idx_node_sync_events_node_id ON node_sync_events(node_id);
CREATE INDEX idx_usage_snapshots_node_id ON usage_snapshots(node_id);
