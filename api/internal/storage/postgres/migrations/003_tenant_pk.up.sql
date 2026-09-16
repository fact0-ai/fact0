-- 003_tenant_pk.up.sql
-- Decouple tenants.id from external identity providers (Clerk org id).
-- After this migration, tenants get an internal opaque id (`tnt_*`) and
-- a separate tenant_external_identities table maps (provider, external_id)
-- to the internal id. Swapping auth providers becomes a config change
-- instead of a primary-key rename across audit_events / api_keys.

BEGIN;

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

COMMIT;
