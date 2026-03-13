ALTER TABLE members
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD COLUMN quota_bytes_limit BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN disabled_reason TEXT NOT NULL DEFAULT '';
