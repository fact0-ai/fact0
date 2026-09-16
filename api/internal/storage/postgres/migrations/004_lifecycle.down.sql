-- 004_lifecycle.down.sql

BEGIN;

DROP TABLE IF EXISTS processed_webhooks;
DROP INDEX IF EXISTS idx_tenants_active;
ALTER TABLE tenants DROP COLUMN IF EXISTS deleted_at;

COMMIT;
