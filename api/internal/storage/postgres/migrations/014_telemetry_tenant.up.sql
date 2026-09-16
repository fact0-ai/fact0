-- Scope execution telemetry to tenants (workspaces).

BEGIN;

ALTER TABLE executions ADD COLUMN IF NOT EXISTS tenant_id TEXT REFERENCES tenants(id);

-- Backfill existing rows to the oldest tenant (Agent 0 proxy).
UPDATE executions
SET tenant_id = (SELECT id FROM tenants ORDER BY created_at ASC LIMIT 1)
WHERE tenant_id IS NULL;

ALTER TABLE executions ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_executions_tenant_started
    ON executions(tenant_id, started_at DESC);

COMMIT;
