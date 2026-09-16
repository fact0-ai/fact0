-- 001_initial_schema.up.sql
-- Core schema for LedgerFlow execution tracing.
-- All execution data tables are append-only by design.

BEGIN;

-- ─── Executions ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS executions (
    id              TEXT        PRIMARY KEY,
    agent_id        TEXT        NOT NULL,
    agent_name      TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'RUNNING',
    root_span_id    TEXT,
    trigger         TEXT        NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    metadata        JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sequence_number BIGSERIAL   NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_executions_agent ON executions(agent_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_started ON executions(started_at DESC);

-- ─── Spans ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS spans (
    id              TEXT        PRIMARY KEY,
    execution_id    TEXT        NOT NULL REFERENCES executions(id),
    parent_span_id  TEXT        REFERENCES spans(id),
    span_type       TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'STARTED',
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    metadata        JSONB       NOT NULL DEFAULT '{}',
    detail          JSONB       NOT NULL DEFAULT '{}',
    error_info      JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sequence_number BIGSERIAL   NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_spans_execution ON spans(execution_id);
CREATE INDEX IF NOT EXISTS idx_spans_parent ON spans(parent_span_id);
CREATE INDEX IF NOT EXISTS idx_spans_type ON spans(execution_id, span_type);

-- ─── Span Causality (DAG edges) ────────────────────────────
CREATE TABLE IF NOT EXISTS span_causality (
    span_id      TEXT NOT NULL REFERENCES spans(id),
    caused_by_id TEXT NOT NULL REFERENCES spans(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (span_id, caused_by_id)
);

-- ─── Execution Events (append-only log) ────────────────────
CREATE TABLE IF NOT EXISTS execution_events (
    id              TEXT        PRIMARY KEY,
    execution_id    TEXT        NOT NULL REFERENCES executions(id),
    span_id         TEXT        NOT NULL REFERENCES spans(id),
    event_type      TEXT        NOT NULL,
    payload_ref     TEXT,
    payload_inline  JSONB,
    payload_size    BIGINT      NOT NULL DEFAULT 0,
    content_type    TEXT        NOT NULL DEFAULT 'application/json',
    timestamp       TIMESTAMPTZ NOT NULL,
    metadata        JSONB       NOT NULL DEFAULT '{}',
    sequence_number BIGSERIAL   NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_execution_seq ON execution_events(execution_id, sequence_number);
CREATE INDEX IF NOT EXISTS idx_events_span ON execution_events(span_id);
CREATE INDEX IF NOT EXISTS idx_events_type ON execution_events(execution_id, event_type);

-- ─── Append-only enforcement ────────────────────────────────
-- Prevent updates on execution_events (append-only)
CREATE OR REPLACE FUNCTION prevent_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'execution_events is append-only: updates are not permitted';
END;
$$ LANGUAGE plpgsql;

-- CREATE TRIGGER has no IF NOT EXISTS form pre-PG14, so we DROP-first
-- to keep the migration idempotent (safe to re-apply).
DROP TRIGGER IF EXISTS trg_events_no_update ON execution_events;
CREATE TRIGGER trg_events_no_update
    BEFORE UPDATE ON execution_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

DROP TRIGGER IF EXISTS trg_events_no_delete ON execution_events;
CREATE TRIGGER trg_events_no_delete
    BEFORE DELETE ON execution_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

COMMIT;
