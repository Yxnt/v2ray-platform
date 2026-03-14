-- Add UNIQUE(node_id, member_uuid) constraints so that ON CONFLICT DO NOTHING
-- actually deduplicates per-node per-member (previously the conflict target was only
-- the primary key, allowing duplicate rows for the same node+member pair).
ALTER TABLE pending_user_removals
    ADD CONSTRAINT pending_user_removals_node_member_unique UNIQUE (node_id, member_uuid);

ALTER TABLE pending_user_additions
    ADD CONSTRAINT pending_user_additions_node_member_unique UNIQUE (node_id, member_uuid);
