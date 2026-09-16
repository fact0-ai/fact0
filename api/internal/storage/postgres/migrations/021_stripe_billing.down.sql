-- 021_stripe_billing.down.sql
-- Remove transactions table and Stripe billing fields from tenants.

BEGIN;

DROP TABLE IF EXISTS transactions;

ALTER TABLE tenants
    DROP COLUMN IF EXISTS stripe_customer_id,
    DROP COLUMN IF EXISTS stripe_subscription_id,
    DROP COLUMN IF EXISTS stripe_price_id,
    DROP COLUMN IF EXISTS subscription_status,
    DROP COLUMN IF EXISTS subscription_current_period_end,
    DROP COLUMN IF EXISTS subscription_cancel_at_period_end;

COMMIT;
