-- 024_trial_emails_status.down.sql
BEGIN;

ALTER TABLE tenants
    DROP COLUMN IF EXISTS trial_reminder_sent,
    DROP COLUMN IF EXISTS trial_expired_email_sent;

COMMIT;
