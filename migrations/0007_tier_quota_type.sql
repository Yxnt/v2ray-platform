-- Add quota_type to tiers: 'monthly' resets each calendar month, 'fixed' is all-time.
ALTER TABLE tiers
    ADD COLUMN IF NOT EXISTS quota_type TEXT NOT NULL DEFAULT 'monthly'
        CHECK (quota_type IN ('monthly', 'fixed'));
