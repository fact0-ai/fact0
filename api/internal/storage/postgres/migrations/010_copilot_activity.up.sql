-- 010_copilot_activity.up.sql
-- Copilot operational telemetry - separate from compliance audit_events
-- (which are for client SDK / agent actions only).

BEGIN;

CREATE TABLE IF NOT EXISTS copilot_activity (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id),
    session_id  TEXT        NOT NULL REFERENCES copilot_sessions(id) ON DELETE CASCADE,
    user_id     TEXT        NOT NULL,
    action      TEXT        NOT NULL,
    actor_type  TEXT        NOT NULL CHECK (actor_type IN ('human', 'agent', 'system')),
    outcome     TEXT        NOT NULL DEFAULT 'success' CHECK (outcome IN ('success', 'failure', 'error')),
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_copilot_activity_tenant_time
    ON copilot_activity (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_copilot_activity_session
    ON copilot_activity (session_id, created_at);

COMMIT;
