BEGIN;

DROP TRIGGER IF EXISTS trg_audit_no_delete ON audit_events;
DROP TRIGGER IF EXISTS trg_audit_no_update ON audit_events;

DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS tenants;

COMMIT;
