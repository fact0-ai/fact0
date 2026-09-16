-- 017_copilot_quota.up.sql
-- Monthly copilot message quota per plan + per-user usage tracking.

BEGIN;

-- Extend plans table with copilot message limit per month.
-- NULL means unlimited (paid / custom tiers).
ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS monthly_copilot_limit INT;

UPDATE plans SET monthly_copilot_limit = 6    WHERE id = 'free';
UPDATE plans SET monthly_copilot_limit = NULL  WHERE id = 'custom';

-- Per-user monthly usage counter, keyed by (tenant, user, YYYY-MM).
-- Upserted on every assistant response so we never over-count on retries.
CREATE TABLE IF NOT EXISTS copilot_usage (
    tenant_id     TEXT        NOT NULL REFERENCES tenants(id),
    user_id       TEXT        NOT NULL,
    period        TEXT        NOT NULL,   -- "2025-05"
    message_count INT         NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id, period)
);

COMMIT;
