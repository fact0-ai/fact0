-- 007_retention.up.sql
-- Free-tier retention: every tenant defaults to the `free` plan with a
-- 30-day window. Older execution data (executions, spans, events) is
-- physically deleted by the retention sweeper. Older audit_events are
-- TOMBSTONED in place - the hash chain stays intact because
-- ComputeHash() does NOT include the columns we scrub (see hash.go).
--
-- To downgrade a tenant to a paid/custom plan, UPDATE tenants.plan_id.
-- A NULL retention_days on the plan disables sweeping for tenants on
-- that plan.

BEGIN;

-- ─── Plans ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS plans (
    id              TEXT        PRIMARY KEY,
    name            TEXT        NOT NULL,
    retention_days  INT,                                  -- NULL = unlimited
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the two built-in plans. ON CONFLICT keeps the migration idempotent.
INSERT INTO plans (id, name, retention_days, description) VALUES
    ('free',   'Free',   30,   'Free tier with 30-day retention.'),
    ('custom', 'Custom', NULL, 'Negotiated retention (talk to sales).')
ON CONFLICT (id) DO NOTHING;

-- ─── Tenants.plan_id ────────────────────────────────────────
-- New tenants are created on `free` by default. Existing tenants are
-- backfilled to `free`. Change via UPDATE for paid customers.
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS plan_id TEXT NOT NULL DEFAULT 'free'
    REFERENCES plans(id);

-- ─── Smart append-only trigger for audit_events ─────────────
-- Original `prevent_event_mutation` rejected ALL updates. We need to
-- allow the retention sweeper to scrub `actor_email`, `resource_name`,
-- and `metadata` without breaking the chain. Replace the trigger with
-- a version that rejects only changes to chain-relevant columns. The
-- hash recipe (audit/hash.go) covers:
--   id, timestamp, actor_id, action, resource_id, outcome, prev_hash
-- plus tenant_id and sequence_number which are also chain identity.
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

DROP TRIGGER IF EXISTS trg_audit_no_update ON audit_events;
CREATE TRIGGER trg_audit_no_update
    BEFORE UPDATE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION audit_events_reject_chain_changes();

-- audit_events DELETE remains blocked by trg_audit_no_delete. Audit
-- rows are never deleted, only tombstoned.

-- ─── Allow telemetry deletes for retention ──────────────────
-- The original migration installed `trg_events_no_delete` on
-- execution_events to discourage accidental deletes. Retention has to
-- delete by design. Drop the trigger - UPDATE-protection (immutable
-- events) stays.
DROP TRIGGER IF EXISTS trg_events_no_delete ON execution_events;

COMMIT;
