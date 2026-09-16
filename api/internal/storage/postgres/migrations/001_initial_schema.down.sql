-- 001_initial_schema.down.sql

BEGIN;

DROP TRIGGER IF EXISTS trg_events_no_delete ON execution_events;
DROP TRIGGER IF EXISTS trg_events_no_update ON execution_events;
DROP FUNCTION IF EXISTS prevent_event_mutation();

DROP TABLE IF EXISTS execution_events;
DROP TABLE IF EXISTS span_causality;
DROP TABLE IF EXISTS spans;
DROP TABLE IF EXISTS executions;

COMMIT;
