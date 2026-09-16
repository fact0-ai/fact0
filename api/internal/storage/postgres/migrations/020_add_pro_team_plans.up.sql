-- 020_add_pro_team_plans.up.sql
-- Seed the new Pro and Team plans into the database plans table.

BEGIN;

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

COMMIT;
