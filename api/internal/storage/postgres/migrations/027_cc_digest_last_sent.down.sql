BEGIN;
ALTER TABLE tenants DROP COLUMN IF EXISTS cc_digest_last_sent;
COMMIT;
