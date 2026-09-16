-- 021_stripe_billing.up.sql
-- Add Stripe billing fields to tenants and create transactions history table for production.

BEGIN;

-- Add Stripe billing fields to tenants
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS stripe_customer_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS stripe_price_id TEXT,
    ADD COLUMN IF NOT EXISTS subscription_status TEXT,
    ADD COLUMN IF NOT EXISTS subscription_current_period_end TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS subscription_cancel_at_period_end BOOLEAN DEFAULT FALSE;

-- Create transaction history table
CREATE TABLE IF NOT EXISTS transactions (
    id TEXT PRIMARY KEY, -- tx_... or invoice_...
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    stripe_invoice_id TEXT UNIQUE,
    amount_cents BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'usd',
    status TEXT NOT NULL,
    hosted_invoice_url TEXT,
    invoice_pdf TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transactions_tenant ON transactions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_transactions_created ON transactions(created_at DESC);

COMMIT;
