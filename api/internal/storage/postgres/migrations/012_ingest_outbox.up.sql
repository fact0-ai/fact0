-- 012_ingest_outbox.up.sql
-- Async audit ingest: durable outbox + client-visible receipts.

BEGIN;

CREATE TABLE IF NOT EXISTS ingest_receipts (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    status          TEXT        NOT NULL CHECK (status IN ('queued', 'committed', 'failed')),
    accepted_count  INT         NOT NULL DEFAULT 0,
    rejected_count  INT         NOT NULL DEFAULT 0,
    event_ids       JSONB       NOT NULL DEFAULT '[]',
    errors          JSONB       NOT NULL DEFAULT '[]',
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    committed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ingest_receipts_tenant
    ON ingest_receipts (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ingest_outbox (
    id              TEXT        PRIMARY KEY,
    receipt_id      TEXT        NOT NULL REFERENCES ingest_receipts(id) ON DELETE CASCADE,
    tenant_id       TEXT        NOT NULL,
    payload         JSONB       NOT NULL,
    status          TEXT        NOT NULL CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    available_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ,
    error_message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_ingest_outbox_poll
    ON ingest_outbox (status, available_at)
    WHERE status = 'pending';

COMMIT;
