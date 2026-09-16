-- 005_better_auth.down.sql
-- Drops all Better Auth tables. ON DELETE CASCADE chains take care
-- of dependent rows for us. Order matters: leaves first.

BEGIN;

drop table if exists "invitation";
drop table if exists "member";
drop table if exists "organization";
drop table if exists "jwks";
drop table if exists "verification";
drop table if exists "account";
drop table if exists "session";
drop table if exists "user";

COMMIT;
