-- pending_user_additions: UUIDs to be dynamically re-added to V2Ray on the next heartbeat.
-- The node-agent reads and clears this list, calling "v2ray api adui" for each entry
-- to restore a member's access in the running V2Ray instance immediately.
CREATE TABLE IF NOT EXISTS pending_user_additions (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    node_id      TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    member_uuid  TEXT NOT NULL,
    member_email TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS pending_user_additions_node_id ON pending_user_additions(node_id);
