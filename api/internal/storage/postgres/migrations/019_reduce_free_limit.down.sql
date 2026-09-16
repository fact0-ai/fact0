-- 019_reduce_free_limit.down.sql
-- Revert free plan limit back to 50,000.

BEGIN;

UPDATE plans SET monthly_event_limit = 50000 WHERE id = 'free';

COMMIT;
