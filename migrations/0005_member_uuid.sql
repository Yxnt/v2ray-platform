-- Add a stable UUID to each member; this UUID is reused as the VMess client ID
-- on every node, making multi-node client configuration straightforward.
ALTER TABLE members
    ADD COLUMN uuid UUID NOT NULL DEFAULT gen_random_uuid();

-- Backfill existing node_credentials so their credential_uuid matches the
-- owning member's UUID.
UPDATE node_credentials nc
SET    credential_uuid = m.uuid
FROM   members m
WHERE  m.id = nc.member_id;

-- The global uniqueness on credential_uuid no longer makes sense (same member
-- UUID appears once per node), so replace it with a per-node uniqueness
-- constraint on (node_id, member_id).
ALTER TABLE node_credentials
    DROP CONSTRAINT IF EXISTS node_credentials_credential_uuid_key;

ALTER TABLE node_credentials
    ADD CONSTRAINT node_credentials_node_member_unique UNIQUE (node_id, member_id);
