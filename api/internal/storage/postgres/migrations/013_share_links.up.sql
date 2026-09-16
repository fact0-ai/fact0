-- 013_share_links.up.sql
-- Read-only share links for external auditors + tenant alert settings.

BEGIN;

CREATE TABLE IF NOT EXISTS share_links (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    created_by      TEXT        NOT NULL,
    label           TEXT        NOT NULL DEFAULT '',
    scope           TEXT        NOT NULL DEFAULT 'read' CHECK (scope IN ('read')),
    filter_from     TIMESTAMPTZ,
    filter_to       TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    last_accessed   TIMESTAMPTZ,
    access_count    INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_share_links_tenant
    ON share_links(tenant_id, created_at DESC);

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}';

COMMIT;
