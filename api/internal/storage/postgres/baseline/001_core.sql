-- Fresh-install OSS v1 baseline. Applied atomically by fact0-migrate.
-- Do not edit after release; add tracked forward migrations instead.


-- 001_initial_schema.up.sql
-- Core schema for LedgerFlow execution tracing.
-- All execution data tables are append-only by design.


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


-- 002_audit.up.sql
-- Compliance-grade audit log: tenants, API keys, and per-tenant
-- hash-chained audit_events. Parallel to execution_events.


-- ─── Tenants ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS tenants (
    id          TEXT        PRIMARY KEY,
    name        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── API Keys ───────────────────────────────────────────────
-- key_hash is SHA-256(raw_key) hex-encoded. Raw secret is shown once
-- to the caller at creation time and never persisted.
CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id),
    key_hash    TEXT        NOT NULL UNIQUE,
    scope       TEXT        NOT NULL CHECK (scope IN ('write', 'read')),
    label       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);

-- ─── Audit Events (append-only, hash-chained per tenant) ────
CREATE TABLE IF NOT EXISTS audit_events (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    timestamp       TIMESTAMPTZ NOT NULL,
    actor_id        TEXT        NOT NULL,
    actor_type      TEXT        NOT NULL CHECK (actor_type IN ('human', 'agent', 'system')),
    actor_email     TEXT        NOT NULL DEFAULT '',
    action          TEXT        NOT NULL,
    resource_id     TEXT        NOT NULL,
    resource_type   TEXT        NOT NULL,
    resource_name   TEXT        NOT NULL DEFAULT '',
    outcome         TEXT        NOT NULL CHECK (outcome IN ('success', 'failure', 'error')),
    metadata        JSONB       NOT NULL DEFAULT '{}',
    prev_hash       VARCHAR(71) NOT NULL,
    hash            VARCHAR(71) NOT NULL,
    sequence_number BIGSERIAL   NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, sequence_number)
);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_actor  ON audit_events(tenant_id, actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_action ON audit_events(tenant_id, action);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time   ON audit_events(tenant_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_seq    ON audit_events(tenant_id, sequence_number);

-- ─── Append-only enforcement (reuse function from migration 001) ─
-- DROP-first so re-applying the migration is safe.
DROP TRIGGER IF EXISTS trg_audit_no_update ON audit_events;
CREATE TRIGGER trg_audit_no_update
    BEFORE UPDATE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();

DROP TRIGGER IF EXISTS trg_audit_no_delete ON audit_events;
CREATE TRIGGER trg_audit_no_delete
    BEFORE DELETE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_event_mutation();


-- 003_tenant_pk.up.sql
-- Decouple tenants.id from external identity providers (Clerk org id).
-- After this migration, tenants get an internal opaque id (`tnt_*`) and
-- a separate tenant_external_identities table maps (provider, external_id)
-- to the internal id. Swapping auth providers becomes a config change
-- instead of a primary-key rename across audit_events / api_keys.


-- ─── Phase 1: stage new internal id on tenants ──────────────
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS ulid TEXT;

UPDATE tenants
SET ulid = 'tnt_' || replace(gen_random_uuid()::text, '-', '')
WHERE ulid IS NULL;

ALTER TABLE tenants ALTER COLUMN ulid SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS tenants_ulid_uidx ON tenants(ulid);

-- ─── Phase 2: drop FKs from child tables so we can rewrite values ─
ALTER TABLE api_keys     DROP CONSTRAINT IF EXISTS api_keys_tenant_id_fkey;
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_tenant_id_fkey;

-- ─── Phase 3: rewrite child tenant_id values to use new ulids ───
UPDATE api_keys
SET tenant_id = t.ulid
FROM tenants t
WHERE api_keys.tenant_id = t.id;

-- audit_events has a BEFORE UPDATE trigger (`trg_audit_no_update`)
-- that enforces the append-only invariant. We must rewrite tenant_id
-- here - but this is safe to do without weakening compliance because
-- the per-tenant SHA-256 hash chain is computed over (actor, action,
-- resource, outcome, metadata, timestamp, prev_hash) and DOES NOT
-- include tenant_id. Rewriting the scoping FK leaves every stored
-- `hash` value valid and GET /v1/verify still passes end-to-end.
-- We disable the trigger only for the duration of this transaction
-- and re-enable it inside the same tx before COMMIT, so the table
-- is never in a writable state outside this controlled migration.
ALTER TABLE audit_events DISABLE TRIGGER trg_audit_no_update;

UPDATE audit_events
SET tenant_id = t.ulid
FROM tenants t
WHERE audit_events.tenant_id = t.id;

ALTER TABLE audit_events ENABLE TRIGGER trg_audit_no_update;

-- ─── Phase 4: external identity mapping ─────────────────────
CREATE TABLE IF NOT EXISTS tenant_external_identities (
    provider    TEXT        NOT NULL,
    external_id TEXT        NOT NULL,
    tenant_id   TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, external_id)
);

-- Seed mapping from existing rows. Pre-migration, tenants.id WAS the
-- Clerk org id, so 'clerk_org' is the only provider that exists.
INSERT INTO tenant_external_identities (provider, external_id, tenant_id)
SELECT 'clerk_org', id, ulid FROM tenants
ON CONFLICT DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_tenant_ext_tenant
    ON tenant_external_identities(tenant_id);

-- ─── Phase 5: swap tenants PK from id -> ulid ───────────────
ALTER TABLE tenants DROP CONSTRAINT IF EXISTS tenants_pkey;
ALTER TABLE tenants DROP COLUMN id;
ALTER TABLE tenants RENAME COLUMN ulid TO id;
ALTER TABLE tenants ADD PRIMARY KEY (id);
DROP INDEX IF EXISTS tenants_ulid_uidx;

-- ─── Phase 6: re-establish FKs (now pointing at new tenants.id) ──
ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE tenant_external_identities
    ADD CONSTRAINT tenant_external_identities_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;


-- 004_lifecycle.up.sql
-- Tenant lifecycle support: soft-delete column on tenants (so we
-- preserve the audit chain even when an org disappears in Clerk) and
-- an idempotency table for inbound webhook deliveries (Svix replays
-- intermittently - we must process each message at most once).


ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tenants_active
    ON tenants(id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS processed_webhooks (
    id           TEXT        PRIMARY KEY,
    source       TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_processed_webhooks_received
    ON processed_webhooks(received_at DESC);


-- 005_better_auth.up.sql
-- Better Auth (https://better-auth.com) lives inside the Next.js
-- process and stores its user / session / organization tables in the
-- same Postgres database as the audit tables. This migration is
-- generated by `npx @better-auth/cli generate` and committed verbatim
-- so we have a reproducible, version-controlled schema.
--
-- To regenerate after changing plugins:
--   cd web && npx @better-auth/cli generate --output ../api/internal/storage/postgres/migrations/005_better_auth.up.sql


create table if not exists "user" (
    "id" text not null primary key,
    "name" text not null,
    "email" text not null unique,
    "emailVerified" boolean not null,
    "image" text,
    "createdAt" timestamptz default CURRENT_TIMESTAMP not null,
    "updatedAt" timestamptz default CURRENT_TIMESTAMP not null
);

create table if not exists "session" (
    "id" text not null primary key,
    "expiresAt" timestamptz not null,
    "token" text not null unique,
    "createdAt" timestamptz default CURRENT_TIMESTAMP not null,
    "updatedAt" timestamptz not null,
    "ipAddress" text,
    "userAgent" text,
    "userId" text not null references "user" ("id") on delete cascade,
    "activeOrganizationId" text
);

create table if not exists "account" (
    "id" text not null primary key,
    "accountId" text not null,
    "providerId" text not null,
    "userId" text not null references "user" ("id") on delete cascade,
    "accessToken" text,
    "refreshToken" text,
    "idToken" text,
    "accessTokenExpiresAt" timestamptz,
    "refreshTokenExpiresAt" timestamptz,
    "scope" text,
    "password" text,
    "createdAt" timestamptz default CURRENT_TIMESTAMP not null,
    "updatedAt" timestamptz not null
);

create table if not exists "verification" (
    "id" text not null primary key,
    "identifier" text not null,
    "value" text not null,
    "expiresAt" timestamptz not null,
    "createdAt" timestamptz default CURRENT_TIMESTAMP not null,
    "updatedAt" timestamptz default CURRENT_TIMESTAMP not null
);

create table if not exists "organization" (
    "id" text not null primary key,
    "name" text not null,
    "slug" text not null unique,
    "logo" text,
    "createdAt" timestamptz not null,
    "metadata" text
);

create table if not exists "member" (
    "id" text not null primary key,
    "organizationId" text not null references "organization" ("id") on delete cascade,
    "userId" text not null references "user" ("id") on delete cascade,
    "role" text not null,
    "createdAt" timestamptz not null
);

create table if not exists "invitation" (
    "id" text not null primary key,
    "organizationId" text not null references "organization" ("id") on delete cascade,
    "email" text not null,
    "role" text,
    "status" text not null,
    "expiresAt" timestamptz not null,
    "createdAt" timestamptz default CURRENT_TIMESTAMP not null,
    "inviterId" text not null references "user" ("id") on delete cascade
);

create table if not exists "jwks" (
    "id" text not null primary key,
    "publicKey" text not null,
    "privateKey" text not null,
    "createdAt" timestamptz not null,
    "expiresAt" timestamptz
);

create index if not exists "session_userId_idx" on "session" ("userId");
create index if not exists "account_userId_idx" on "account" ("userId");
create index if not exists "verification_identifier_idx" on "verification" ("identifier");
create unique index if not exists "organization_slug_uidx" on "organization" ("slug");
create index if not exists "member_organizationId_idx" on "member" ("organizationId");
create index if not exists "member_userId_idx" on "member" ("userId");
create index if not exists "invitation_organizationId_idx" on "invitation" ("organizationId");
create index if not exists "invitation_email_idx" on "invitation" ("email");


-- 006_redaction.up.sql
-- Per-tenant toggle for ingest-time PII / secret redaction. Defaults
-- to ON for every existing and future tenant; an admin can disable it
-- via a future Settings → Compliance switch.
--
-- Redaction itself runs inside the API process (see
-- api/internal/redaction) - this column is purely the policy.


ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS redaction_enabled BOOLEAN NOT NULL DEFAULT true;


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


-- 009_copilot.up.sql
-- Copilot session resume + full transcript storage with plan-based retention.


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


-- 010_copilot_activity.up.sql
-- Copilot operational telemetry - separate from compliance audit_events
-- (which are for client SDK / agent actions only).


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


-- 011_platform_role.up.sql
-- Global platform admin role on Better Auth users (separate from org member.role).


ALTER TABLE "user"
    ADD COLUMN IF NOT EXISTS "platformRole" TEXT
    CHECK ("platformRole" IS NULL OR "platformRole" IN ('founder', 'support', 'viewer'));

CREATE INDEX IF NOT EXISTS idx_user_platform_role ON "user" ("platformRole")
    WHERE "platformRole" IS NOT NULL;


-- 012_ingest_outbox.up.sql
-- Async audit ingest: durable outbox + client-visible receipts.


CREATE TABLE IF NOT EXISTS ingest_receipts (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    status          TEXT        NOT NULL CHECK (status IN ('queued', 'committed', 'failed')),
    accepted_count  INT         NOT NULL DEFAULT 0,
    rejected_count  INT         NOT NULL DEFAULT 0,
    event_ids       JSONB       NOT NULL DEFAULT '[]',
    errors          JSONB       NOT NULL DEFAULT '[]',
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    committed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ingest_receipts_tenant
    ON ingest_receipts (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ingest_outbox (
    id              TEXT        PRIMARY KEY,
    receipt_id      TEXT        NOT NULL REFERENCES ingest_receipts(id) ON DELETE CASCADE,
    tenant_id       TEXT        NOT NULL,
    payload         JSONB       NOT NULL,
    status          TEXT        NOT NULL CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    available_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ,
    error_message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_ingest_outbox_poll
    ON ingest_outbox (status, available_at)
    WHERE status = 'pending';


-- 013_share_links.up.sql
-- Read-only share links for external auditors + tenant alert settings.


CREATE TABLE IF NOT EXISTS share_links (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    created_by      TEXT        NOT NULL,
    label           TEXT        NOT NULL DEFAULT '',
    scope           TEXT        NOT NULL DEFAULT 'read' CHECK (scope IN ('read')),
    filter_from     TIMESTAMPTZ,
    filter_to       TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    last_accessed   TIMESTAMPTZ,
    access_count    INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_share_links_tenant
    ON share_links(tenant_id, created_at DESC);

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}';


-- Scope execution telemetry to tenants (workspaces).


ALTER TABLE executions ADD COLUMN IF NOT EXISTS tenant_id TEXT REFERENCES tenants(id);

-- Backfill existing rows to the oldest tenant (Agent 0 proxy).
UPDATE executions
SET tenant_id = (SELECT id FROM tenants ORDER BY created_at ASC LIMIT 1)
WHERE tenant_id IS NULL;

ALTER TABLE executions ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_executions_tenant_started
    ON executions(tenant_id, started_at DESC);


-- 015_plan_limits.up.sql
-- Monthly audit event cap for the free plan (NULL = unlimited on custom).


ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS monthly_event_limit BIGINT;

UPDATE plans SET monthly_event_limit = 50000 WHERE id = 'free';
UPDATE plans SET monthly_event_limit = NULL WHERE id = 'custom';


-- 016_fact0_reanchor_guc.up.sql
-- Rename session GUC ledgerflow.reanchor_mode → fact0.reanchor_mode.


CREATE OR REPLACE FUNCTION audit_events_reject_chain_changes()
RETURNS TRIGGER AS $$
DECLARE
    reanchor_mode TEXT;
BEGIN
    BEGIN
        reanchor_mode := current_setting('fact0.reanchor_mode', true);
    EXCEPTION WHEN OTHERS THEN
        reanchor_mode := '';
    END;

    IF reanchor_mode = 'on' THEN
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


-- 017_copilot_quota.up.sql
-- Monthly copilot message quota per plan + per-user usage tracking.


-- Extend plans table with copilot message limit per month.
-- NULL means unlimited (paid / custom tiers).
ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS monthly_copilot_limit INT;

UPDATE plans SET monthly_copilot_limit = 6    WHERE id = 'free';
UPDATE plans SET monthly_copilot_limit = NULL  WHERE id = 'custom';

-- Per-user monthly usage counter, keyed by (tenant, user, YYYY-MM).
-- Upserted on every assistant response so we never over-count on retries.
CREATE TABLE IF NOT EXISTS copilot_usage (
    tenant_id     TEXT        NOT NULL REFERENCES tenants(id),
    user_id       TEXT        NOT NULL,
    period        TEXT        NOT NULL,   -- "2025-05"
    message_count INT         NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id, period)
);


-- 018_user_welcome_email.up.sql
-- Tracks one-time welcome email per Better Auth user account.


ALTER TABLE "user"
    ADD COLUMN IF NOT EXISTS "welcomeEmailSentAt" timestamptz;


-- 019_reduce_free_limit.up.sql
-- Reduce monthly audit event cap for the free plan from 50,000 to 30,000.


UPDATE plans SET monthly_event_limit = 30000 WHERE id = 'free';


-- 020_add_pro_team_plans.up.sql
-- Seed the new Pro and Team plans into the database plans table.


INSERT INTO plans (id, name, retention_days, copilot_transcript_retention_days, description, monthly_event_limit, monthly_copilot_limit) VALUES
    ('pro',  'Pro',  90,  90,  'For teams shipping AI agents in production and approaching their first enterprise deal.', 200000,  NULL),
    ('team', 'Team', 365, 365, 'For organizations running multiple agent workspaces that need controls and collaboration.', 1000000, NULL)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    retention_days = EXCLUDED.retention_days,
    copilot_transcript_retention_days = EXCLUDED.copilot_transcript_retention_days,
    description = EXCLUDED.description,
    monthly_event_limit = EXCLUDED.monthly_event_limit,
    monthly_copilot_limit = EXCLUDED.monthly_copilot_limit;


-- 021_stripe_billing.up.sql
-- Add Stripe billing fields to tenants and create transactions history table for production.


-- Add Stripe billing fields to tenants
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS stripe_customer_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS stripe_price_id TEXT,
    ADD COLUMN IF NOT EXISTS subscription_status TEXT,
    ADD COLUMN IF NOT EXISTS subscription_current_period_end TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS subscription_cancel_at_period_end BOOLEAN DEFAULT FALSE;

-- Create transaction history table
CREATE TABLE IF NOT EXISTS transactions (
    id TEXT PRIMARY KEY, -- tx_... or invoice_...
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    stripe_invoice_id TEXT UNIQUE,
    amount_cents BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'usd',
    status TEXT NOT NULL,
    hosted_invoice_url TEXT,
    invoice_pdf TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transactions_tenant ON transactions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_transactions_created ON transactions(created_at DESC);


ALTER TABLE share_links ADD COLUMN secret_hash VARCHAR(64);

-- 023_immutable_audit_columns.up.sql
-- Enforce database immutability for actor_email, resource_name, and metadata columns.


CREATE OR REPLACE FUNCTION audit_events_reject_chain_changes()
RETURNS TRIGGER AS $$
DECLARE
    reanchor_mode TEXT;
BEGIN
    BEGIN
        reanchor_mode := current_setting('fact0.reanchor_mode', true);
    EXCEPTION WHEN OTHERS THEN
        reanchor_mode := '';
    END;

    IF reanchor_mode = 'on' THEN
        IF NEW.id              IS DISTINCT FROM OLD.id             OR
           NEW.tenant_id       IS DISTINCT FROM OLD.tenant_id      OR
           NEW.timestamp       IS DISTINCT FROM OLD.timestamp      OR
           NEW.actor_id        IS DISTINCT FROM OLD.actor_id       OR
           NEW.actor_type      IS DISTINCT FROM OLD.actor_type     OR
           NEW.actor_email     IS DISTINCT FROM OLD.actor_email    OR
           NEW.action          IS DISTINCT FROM OLD.action         OR
           NEW.resource_id     IS DISTINCT FROM OLD.resource_id    OR
           NEW.resource_type   IS DISTINCT FROM OLD.resource_type  OR
           NEW.resource_name   IS DISTINCT FROM OLD.resource_name  OR
           NEW.outcome         IS DISTINCT FROM OLD.outcome        OR
           NEW.metadata        IS DISTINCT FROM OLD.metadata       OR
           NEW.sequence_number IS DISTINCT FROM OLD.sequence_number
        THEN
            RAISE EXCEPTION 'audit_events: data columns are immutable even during re-anchor';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.id            IS DISTINCT FROM OLD.id            OR
       NEW.tenant_id     IS DISTINCT FROM OLD.tenant_id     OR
       NEW.timestamp     IS DISTINCT FROM OLD.timestamp     OR
       NEW.actor_id      IS DISTINCT FROM OLD.actor_id      OR
       NEW.actor_type    IS DISTINCT FROM OLD.actor_type    OR
       NEW.actor_email   IS DISTINCT FROM OLD.actor_email   OR
       NEW.action        IS DISTINCT FROM OLD.action        OR
       NEW.resource_id   IS DISTINCT FROM OLD.resource_id   OR
       NEW.resource_type IS DISTINCT FROM OLD.resource_type OR
       NEW.resource_name IS DISTINCT FROM OLD.resource_name OR
       NEW.outcome       IS DISTINCT FROM OLD.outcome       OR
       NEW.metadata      IS DISTINCT FROM OLD.metadata      OR
       NEW.prev_hash     IS DISTINCT FROM OLD.prev_hash     OR
       NEW.hash          IS DISTINCT FROM OLD.hash          OR
       NEW.sequence_number IS DISTINCT FROM OLD.sequence_number
    THEN
        RAISE EXCEPTION 'audit_events: chain-relevant columns are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- 024_trial_emails_status.up.sql

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS trial_reminder_sent BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS trial_expired_email_sent BOOLEAN DEFAULT FALSE;


-- 025_prompt_catalog.up.sql

CREATE TABLE IF NOT EXISTS prompt_catalog (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    version      INT NOT NULL DEFAULT 1,
    template     TEXT NOT NULL,
    variables    TEXT[] DEFAULT '{}',
    model_hints  TEXT[] DEFAULT '{}',
    metadata     JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name, version)
);

CREATE INDEX IF NOT EXISTS idx_prompt_catalog_tenant ON prompt_catalog(tenant_id);


-- 026_claude_code_policies.up.sql
-- Per-tenant governance policy for the Claude Code plugin. One row per
-- tenant; version enables compare-and-swap writes and cheap 304 polling.
-- History lives in the tamper-evident audit chain (claude_code.policy.updated
-- events), not here.

CREATE TABLE IF NOT EXISTS claude_code_policies (
    tenant_id   TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    version     INT NOT NULL DEFAULT 1,
    enforce     BOOLEAN NOT NULL DEFAULT false,
    rules       JSONB NOT NULL DEFAULT '[]',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT NOT NULL DEFAULT ''
);


-- 027_cc_digest_last_sent.up.sql
-- Sent-marker for the weekly coding-agents digest. A dedicated column (not
-- tenants.settings JSONB) so multiple API replicas can claim atomically
-- (UPDATE ... WHERE cc_digest_last_sent IS NULL OR < cutoff).

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS cc_digest_last_sent TIMESTAMPTZ;



-- OSS invariants. Audit/history are append-only even when old GUCs are set.
ALTER TABLE audit_events ALTER COLUMN hash TYPE text;
ALTER TABLE audit_events ALTER COLUMN prev_hash TYPE text;
CREATE OR REPLACE FUNCTION audit_events_reject_chain_changes() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only: updates are not permitted';
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION prevent_event_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'recorded history is append-only: updates and deletes are not permitted';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_audit_no_update ON audit_events;
CREATE TRIGGER trg_audit_no_update BEFORE UPDATE ON audit_events FOR EACH ROW EXECUTE FUNCTION prevent_event_mutation();
DROP TRIGGER IF EXISTS trg_audit_no_delete ON audit_events;
CREATE TRIGGER trg_audit_no_delete BEFORE DELETE ON audit_events FOR EACH ROW EXECUTE FUNCTION prevent_event_mutation();
DROP TRIGGER IF EXISTS trg_events_no_update ON execution_events;
CREATE TRIGGER trg_events_no_update BEFORE UPDATE ON execution_events FOR EACH ROW EXECUTE FUNCTION prevent_event_mutation();
DROP TRIGGER IF EXISTS trg_events_no_delete ON execution_events;
CREATE TRIGGER trg_events_no_delete BEFORE DELETE ON execution_events FOR EACH ROW EXECUTE FUNCTION prevent_event_mutation();
ALTER TABLE spans ADD CONSTRAINT spans_execution_id_id_unique UNIQUE(execution_id,id);
ALTER TABLE spans DROP CONSTRAINT IF EXISTS spans_parent_span_id_fkey;
ALTER TABLE spans ADD CONSTRAINT spans_parent_same_execution FOREIGN KEY(execution_id,parent_span_id) REFERENCES spans(execution_id,id);
ALTER TABLE execution_events DROP CONSTRAINT IF EXISTS execution_events_span_id_fkey;
ALTER TABLE execution_events ADD CONSTRAINT event_span_same_execution FOREIGN KEY(execution_id,span_id) REFERENCES spans(execution_id,id);
CREATE OR REPLACE FUNCTION enforce_causality_execution() RETURNS trigger AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM spans a JOIN spans b ON a.execution_id=b.execution_id WHERE a.id=NEW.span_id AND b.id=NEW.caused_by_id) THEN
    RAISE EXCEPTION 'causality spans must belong to the same execution';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER causality_same_execution BEFORE INSERT OR UPDATE ON span_causality FOR EACH ROW EXECUTE FUNCTION enforce_causality_execution();
CREATE INDEX idx_audit_session ON audit_events(tenant_id, (metadata->>'session_id'), sequence_number);
INSERT INTO plans(id,name,description,retention_days,monthly_event_limit,monthly_copilot_limit)
VALUES ('oss','Self-hosted','All core capabilities; operator-managed storage.',NULL,NULL,NULL) ON CONFLICT(id) DO NOTHING;
ALTER TABLE tenants ALTER COLUMN plan_id SET DEFAULT 'oss';
ALTER TABLE tenants ALTER COLUMN redaction_enabled SET DEFAULT false;
