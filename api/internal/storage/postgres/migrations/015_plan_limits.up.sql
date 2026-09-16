-- 015_plan_limits.up.sql
-- Monthly audit event cap for the free plan (NULL = unlimited on custom).

BEGIN;

ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS monthly_event_limit BIGINT;

UPDATE plans SET monthly_event_limit = 50000 WHERE id = 'free';
UPDATE plans SET monthly_event_limit = NULL WHERE id = 'custom';

COMMIT;
