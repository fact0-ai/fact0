-- 025_prompt_catalog.up.sql
BEGIN;

CREATE TABLE IF NOT EXISTS prompt_catalog (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    version      INT NOT NULL DEFAULT 1,
    template     TEXT NOT NULL,
    variables    TEXT[] DEFAULT '{}',
    model_hints  TEXT[] DEFAULT '{}',
    metadata     JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name, version)
);

CREATE INDEX IF NOT EXISTS idx_prompt_catalog_tenant ON prompt_catalog(tenant_id);

COMMIT;
