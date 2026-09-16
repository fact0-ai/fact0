-- 007_retention.down.sql
BEGIN;

-- Restore original strict no-update trigger on audit_events.
DROP TRIGGER IF EXISTS trg_audit_no_update ON audit_events;
CREATE TRIGGER trg_audit_no_update
    BEFORE UPDATE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

-- Restore the no-delete trigger on execution_events.
DROP TRIGGER IF EXISTS trg_events_no_delete ON execution_events;
CREATE TRIGGER trg_events_no_delete
    BEFORE DELETE ON execution_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

DROP FUNCTION IF EXISTS audit_events_reject_chain_changes();

ALTER TABLE tenants DROP COLUMN IF EXISTS plan_id;
DROP TABLE IF EXISTS plans;

COMMIT;
