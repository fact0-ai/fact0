-- 009_copilot.up.sql
-- Copilot session resume + full transcript storage with plan-based retention.

BEGIN;

ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS copilot_transcript_retention_days INT;

-- Free tier mirrors audit retention; custom stays NULL (unlimited).
UPDATE plans
SET copilot_transcript_retention_days = retention_days
WHERE id = 'free' AND copilot_transcript_retention_days IS NULL;

CREATE TABLE IF NOT EXISTS copilot_sessions (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id),
    user_id     TEXT        NOT NULL,
    org_id      TEXT        NOT NULL,
    title       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_copilot_sessions_resume
    ON copilot_sessions (tenant_id, user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS copilot_messages (
    id          TEXT        PRIMARY KEY,
    session_id  TEXT        NOT NULL REFERENCES copilot_sessions(id) ON DELETE CASCADE,
    tenant_id   TEXT        NOT NULL,
    role        TEXT        NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content     TEXT        NOT NULL DEFAULT '',
    parts       JSONB,
    redacted    BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_copilot_messages_session
    ON copilot_messages (session_id, created_at);

COMMIT;
