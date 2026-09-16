-- 011_platform_role.up.sql
-- Global platform admin role on Better Auth users (separate from org member.role).

BEGIN;

ALTER TABLE "user"
    ADD COLUMN IF NOT EXISTS "platformRole" TEXT
    CHECK ("platformRole" IS NULL OR "platformRole" IN ('founder', 'support', 'viewer'));

CREATE INDEX IF NOT EXISTS idx_user_platform_role ON "user" ("platformRole")
    WHERE "platformRole" IS NOT NULL;

COMMIT;
