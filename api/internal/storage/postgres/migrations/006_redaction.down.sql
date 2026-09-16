-- 006_redaction.down.sql
BEGIN;
ALTER TABLE tenants DROP COLUMN IF EXISTS redaction_enabled;
COMMIT;
