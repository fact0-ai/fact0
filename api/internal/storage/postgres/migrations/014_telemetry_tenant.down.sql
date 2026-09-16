BEGIN;

DROP INDEX IF EXISTS idx_executions_tenant_started;
ALTER TABLE executions DROP COLUMN IF EXISTS tenant_id;

COMMIT;
