-- Restore per-node credential isolation for databases that applied the
-- vulnerable 0005 migration which copied members.uuid into node_credentials.
UPDATE node_credentials nc
SET    credential_uuid = gen_random_uuid()
FROM   members m
WHERE  m.id = nc.member_id
  AND  nc.credential_uuid = m.uuid;

ALTER TABLE node_credentials
    DROP CONSTRAINT IF EXISTS node_credentials_node_member_unique;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'node_credentials_credential_uuid_key'
          AND conrelid = 'node_credentials'::regclass
    ) THEN
        ALTER TABLE node_credentials
            ADD CONSTRAINT node_credentials_credential_uuid_key UNIQUE (credential_uuid);
    END IF;
END $$;
