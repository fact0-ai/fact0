-- 008_reanchor.down.sql
-- Restore the stricter trigger from 007 that disallows hash/prev_hash changes.

BEGIN;

CREATE OR REPLACE FUNCTION audit_events_reject_chain_changes()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id            IS DISTINCT FROM OLD.id            OR
       NEW.tenant_id     IS DISTINCT FROM OLD.tenant_id     OR
       NEW.timestamp     IS DISTINCT FROM OLD.timestamp     OR
       NEW.actor_id      IS DISTINCT FROM OLD.actor_id      OR
       NEW.actor_type    IS DISTINCT FROM OLD.actor_type    OR
       NEW.action        IS DISTINCT FROM OLD.action        OR
       NEW.resource_id   IS DISTINCT FROM OLD.resource_id   OR
       NEW.resource_type IS DISTINCT FROM OLD.resource_type OR
       NEW.outcome       IS DISTINCT FROM OLD.outcome       OR
       NEW.prev_hash     IS DISTINCT FROM OLD.prev_hash     OR
       NEW.hash          IS DISTINCT FROM OLD.hash          OR
       NEW.sequence_number IS DISTINCT FROM OLD.sequence_number
    THEN
        RAISE EXCEPTION 'audit_events: chain-relevant columns are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMIT;
