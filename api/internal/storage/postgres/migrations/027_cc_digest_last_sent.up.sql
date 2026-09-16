-- 027_cc_digest_last_sent.up.sql
-- Sent-marker for the weekly coding-agents digest. A dedicated column (not
-- tenants.settings JSONB) so multiple API replicas can claim atomically
-- (UPDATE ... WHERE cc_digest_last_sent IS NULL OR < cutoff).
BEGIN;

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS cc_digest_last_sent TIMESTAMPTZ;

COMMIT;
