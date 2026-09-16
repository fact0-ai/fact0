-- 020_add_pro_team_plans.down.sql
-- Remove the Pro and Team plans from the database plans table.

BEGIN;

DELETE FROM plans WHERE id IN ('pro', 'team');

COMMIT;
