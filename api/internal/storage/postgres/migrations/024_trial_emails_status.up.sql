-- 024_trial_emails_status.up.sql
BEGIN;

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS trial_reminder_sent BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS trial_expired_email_sent BOOLEAN DEFAULT FALSE;

COMMIT;
