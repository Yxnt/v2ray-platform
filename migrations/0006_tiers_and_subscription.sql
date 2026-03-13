-- Tier definitions: named service levels with monthly traffic quotas.
CREATE TABLE IF NOT EXISTS tiers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- monthly quota in bytes; 0 = unlimited
    quota_bytes BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Add tier and subscription token to members.
ALTER TABLE members
    ADD COLUMN IF NOT EXISTS tier_id            UUID REFERENCES tiers(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS subscription_token UUID NOT NULL DEFAULT gen_random_uuid();

-- Ensure subscription tokens are unique so they can be used as public identifiers.
CREATE UNIQUE INDEX IF NOT EXISTS members_subscription_token_idx ON members(subscription_token);
