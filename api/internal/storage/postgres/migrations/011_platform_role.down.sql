BEGIN;

DROP INDEX IF EXISTS idx_user_platform_role;
ALTER TABLE "user" DROP COLUMN IF EXISTS "platformRole";

COMMIT;
