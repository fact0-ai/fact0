-- db_reset.sql - wipe every row from every app + auth table while
-- keeping the schema intact. Idempotent and FK-safe (RESTART IDENTITY
-- CASCADE chases dependencies).
--
-- Invoked by `make db-reset`. Not a migration - never appears in the
-- migrations directory because the schema itself is unchanged.

BEGIN;

TRUNCATE TABLE
    audit_events,
    execution_events,
    span_causality,
    spans,
    executions,
    api_keys,
    tenant_external_identities,
    processed_webhooks,
    tenants,
    "invitation",
    "member",
    "session",
    "account",
    "verification",
    "jwks",
    "organization",
    "user"
RESTART IDENTITY CASCADE;

COMMIT;
