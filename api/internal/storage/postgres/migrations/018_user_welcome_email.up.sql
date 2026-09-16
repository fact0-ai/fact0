-- 018_user_welcome_email.up.sql
-- Tracks one-time welcome email per Better Auth user account.

BEGIN;

ALTER TABLE "user"
    ADD COLUMN IF NOT EXISTS "welcomeEmailSentAt" timestamptz;

COMMIT;
