-- Add a stable UUID to each member for user-facing subscription identity.
-- Node VMess credentials must remain node-scoped bearer secrets; do not
-- backfill node_credentials from this member UUID.
ALTER TABLE members
    ADD COLUMN uuid UUID NOT NULL DEFAULT gen_random_uuid();
