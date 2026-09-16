-- 012_ingest_outbox.down.sql

BEGIN;

DROP TABLE IF EXISTS ingest_outbox;
DROP TABLE IF EXISTS ingest_receipts;

COMMIT;
