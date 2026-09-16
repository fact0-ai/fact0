-- 017_copilot_quota.down.sql
-- Revert copilot quota schema additions.

BEGIN;

DROP TABLE IF EXISTS copilot_usage;
ALTER TABLE plans DROP COLUMN IF EXISTS monthly_copilot_limit;

COMMIT;
