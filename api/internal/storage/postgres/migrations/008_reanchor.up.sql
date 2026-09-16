-- 008_reanchor.up.sql
-- Relaxes the audit_events immutability trigger so the re-anchor service
-- operation can recompute prev_hash + hash forward from a break point.
--
-- We use a custom GUC (ledgerflow.reanchor_mode) as a session-scoped bypass
-- flag. The trigger allows hash/prev_hash updates ONLY when this GUC is 'on'
-- in the current transaction; all other chain-critical columns remain
-- immutable always.
--
-- The ReanchorFrom store method sets this GUC at the start of its transaction
-- and the trigger check clears it automatically when the transaction ends.

BEGIN;

CREATE OR REPLACE FUNCTION audit_events_reject_chain_changes()
RETURNS TRIGGER AS $$
DECLARE
    reanchor_mode TEXT;
BEGIN
    -- Allow hash/prev_hash updates when the re-anchor GUC is set.
    BEGIN
        reanchor_mode := current_setting('ledgerflow.reanchor_mode', true);
    EXCEPTION WHEN OTHERS THEN
        reanchor_mode := '';
    END;

    IF reanchor_mode = 'on' THEN
        -- In re-anchor mode: only hash and prev_hash may change.
        IF NEW.id              IS DISTINCT FROM OLD.id             OR
           NEW.tenant_id       IS DISTINCT FROM OLD.tenant_id      OR
           NEW.timestamp       IS DISTINCT FROM OLD.timestamp      OR
           NEW.actor_id        IS DISTINCT FROM OLD.actor_id       OR
           NEW.actor_type      IS DISTINCT FROM OLD.actor_type     OR
           NEW.action          IS DISTINCT FROM OLD.action         OR
           NEW.resource_id     IS DISTINCT FROM OLD.resource_id    OR
           NEW.resource_type   IS DISTINCT FROM OLD.resource_type  OR
           NEW.outcome         IS DISTINCT FROM OLD.outcome        OR
           NEW.sequence_number IS DISTINCT FROM OLD.sequence_number
        THEN
            RAISE EXCEPTION 'audit_events: data columns are immutable even during re-anchor';
        END IF;
        RETURN NEW;
    END IF;

    -- Normal mode: all chain-relevant columns are immutable.
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
