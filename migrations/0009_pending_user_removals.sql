-- pending_user_removals: UUIDs to be dynamically removed from V2Ray on the next heartbeat.
-- The node-agent reads and clears this list, calling "v2ray api rmui" for each entry immediately,
-- without waiting for a full config reload.
CREATE TABLE IF NOT EXISTS pending_user_removals (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    node_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    member_uuid TEXT NOT NULL,
    member_email TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS pending_user_removals_node_id ON pending_user_removals(node_id);
