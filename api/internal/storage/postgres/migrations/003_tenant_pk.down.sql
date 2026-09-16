-- 003_tenant_pk.down.sql
-- Reverse the tenant PK refactor. We reconstruct the old "tenants.id ==
-- Clerk org id" shape from tenant_external_identities. Multi-provider
-- mappings collapse to clerk_org only - this down migration is lossy
-- by design and intended for dev rollback, not production downgrade.

BEGIN;

-- Stage the old Clerk org id back onto tenants
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS legacy_id TEXT;

UPDATE tenants t
SET legacy_id = m.external_id
FROM tenant_external_identities m
WHERE m.tenant_id = t.id AND m.provider = 'clerk_org';

UPDATE tenants SET legacy_id = id WHERE legacy_id IS NULL;

ALTER TABLE tenants ALTER COLUMN legacy_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS tenants_legacy_id_uidx ON tenants(legacy_id);

ALTER TABLE api_keys     DROP CONSTRAINT IF EXISTS api_keys_tenant_id_fkey;
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_tenant_id_fkey;

UPDATE api_keys
SET tenant_id = t.legacy_id
FROM tenants t
WHERE api_keys.tenant_id = t.id;

UPDATE audit_events
SET tenant_id = t.legacy_id
FROM tenants t
WHERE audit_events.tenant_id = t.id;

ALTER TABLE tenants DROP CONSTRAINT IF EXISTS tenants_pkey;
ALTER TABLE tenants DROP COLUMN id;
ALTER TABLE tenants RENAME COLUMN legacy_id TO id;
ALTER TABLE tenants ADD PRIMARY KEY (id);
DROP INDEX IF EXISTS tenants_legacy_id_uidx;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

DROP TABLE IF EXISTS tenant_external_identities;

COMMIT;
