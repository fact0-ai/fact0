-- 026_claude_code_policies.up.sql
-- Per-tenant governance policy for the Claude Code plugin. One row per
-- tenant; version enables compare-and-swap writes and cheap 304 polling.
-- History lives in the tamper-evident audit chain (claude_code.policy.updated
-- events), not here.
BEGIN;

CREATE TABLE IF NOT EXISTS claude_code_policies (
    tenant_id   TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    version     INT NOT NULL DEFAULT 1,
    enforce     BOOLEAN NOT NULL DEFAULT false,
    rules       JSONB NOT NULL DEFAULT '[]',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT NOT NULL DEFAULT ''
);

COMMIT;
