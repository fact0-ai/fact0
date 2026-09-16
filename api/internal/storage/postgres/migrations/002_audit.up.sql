-- 002_audit.up.sql
-- Compliance-grade audit log: tenants, API keys, and per-tenant
-- hash-chained audit_events. Parallel to execution_events.

BEGIN;

-- ─── Tenants ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS tenants (
    id          TEXT        PRIMARY KEY,
    name        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── API Keys ───────────────────────────────────────────────
-- key_hash is SHA-256(raw_key) hex-encoded. Raw secret is shown once
-- to the caller at creation time and never persisted.
CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id),
    key_hash    TEXT        NOT NULL UNIQUE,
    scope       TEXT        NOT NULL CHECK (scope IN ('write', 'read')),
    label       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);

-- ─── Audit Events (append-only, hash-chained per tenant) ────
CREATE TABLE IF NOT EXISTS audit_events (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    timestamp       TIMESTAMPTZ NOT NULL,
    actor_id        TEXT        NOT NULL,
    actor_type      TEXT        NOT NULL CHECK (actor_type IN ('human', 'agent', 'system')),
    actor_email     TEXT        NOT NULL DEFAULT '',
    action          TEXT        NOT NULL,
    resource_id     TEXT        NOT NULL,
    resource_type   TEXT        NOT NULL,
    resource_name   TEXT        NOT NULL DEFAULT '',
    outcome         TEXT        NOT NULL CHECK (outcome IN ('success', 'failure', 'error')),
    metadata        JSONB       NOT NULL DEFAULT '{}',
    prev_hash       VARCHAR(71) NOT NULL,
    hash            VARCHAR(71) NOT NULL,
    sequence_number BIGSERIAL   NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, sequence_number)
);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_actor  ON audit_events(tenant_id, actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_action ON audit_events(tenant_id, action);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time   ON audit_events(tenant_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_seq    ON audit_events(tenant_id, sequence_number);

-- ─── Append-only enforcement (reuse function from migration 001) ─
-- DROP-first so re-applying the migration is safe.
DROP TRIGGER IF EXISTS trg_audit_no_update ON audit_events;
CREATE TRIGGER trg_audit_no_update
    BEFORE UPDATE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

DROP TRIGGER IF EXISTS trg_audit_no_delete ON audit_events;
CREATE TRIGGER trg_audit_no_delete
    BEFORE DELETE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

COMMIT;
