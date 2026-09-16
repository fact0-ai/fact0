-- 004_lifecycle.up.sql
-- Tenant lifecycle support: soft-delete column on tenants (so we
-- preserve the audit chain even when an org disappears in Clerk) and
-- an idempotency table for inbound webhook deliveries (Svix replays
-- intermittently - we must process each message at most once).

BEGIN;

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tenants_active
    ON tenants(id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS processed_webhooks (
    id           TEXT        PRIMARY KEY,
    source       TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_processed_webhooks_received
    ON processed_webhooks(received_at DESC);

COMMIT;
