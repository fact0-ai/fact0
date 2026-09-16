-- 019_reduce_free_limit.up.sql
-- Reduce monthly audit event cap for the free plan from 50,000 to 30,000.

BEGIN;

UPDATE plans SET monthly_event_limit = 30000 WHERE id = 'free';

COMMIT;
