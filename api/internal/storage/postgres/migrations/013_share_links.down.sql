-- 013_share_links.down.sql

BEGIN;

ALTER TABLE tenants DROP COLUMN IF EXISTS settings;
DROP TABLE IF EXISTS share_links;

COMMIT;
