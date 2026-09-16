-- 006_redaction.up.sql
-- Per-tenant toggle for ingest-time PII / secret redaction. Defaults
-- to ON for every existing and future tenant; an admin can disable it
-- via a future Settings → Compliance switch.
--
-- Redaction itself runs inside the API process (see
-- api/internal/redaction) - this column is purely the policy.

BEGIN;

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS redaction_enabled BOOLEAN NOT NULL DEFAULT true;

COMMIT;
