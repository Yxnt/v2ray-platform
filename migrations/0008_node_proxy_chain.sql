-- Add optional proxy chain: a node can forward its traffic through another node.
-- Used in Clash config generation: sets `dialer-proxy` on the proxy entry.
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS proxy_node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL;
