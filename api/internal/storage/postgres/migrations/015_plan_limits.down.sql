-- 015_plan_limits.down.sql
BEGIN;

ALTER TABLE plans DROP COLUMN IF EXISTS monthly_event_limit;

COMMIT;
