package audit

import (
	"context"
	"time"
)

// TenantSubscription holds Stripe billing state for a tenant.
type TenantSubscription struct {
	TenantID                      string     `json:"tenant_id"`
	StripeCustomerID              *string    `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID          *string    `json:"stripe_subscription_id,omitempty"`
	StripePriceID                 *string    `json:"stripe_price_id,omitempty"`
	SubscriptionStatus            *string    `json:"subscription_status,omitempty"`
	SubscriptionCurrentPeriodEnd  *time.Time `json:"subscription_current_period_end,omitempty"`
	SubscriptionCancelAtPeriodEnd bool       `json:"subscription_cancel_at_period_end"`
}

// Transaction records a historical customer payment/invoice.
type Transaction struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	StripeInvoiceID  *string   `json:"stripe_invoice_id,omitempty"`
	AmountCents      int64     `json:"amount_cents"`
	Currency         string    `json:"currency"`
	Status           string    `json:"status"`
	HostedInvoiceURL *string   `json:"hosted_invoice_url,omitempty"`
	InvoicePDF       *string   `json:"invoice_pdf,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// BillingStore manages Stripe integration persistence in PostgreSQL.
type BillingStore interface {
	// SetTenantSubscription updates a tenant's Stripe billing info.
	SetTenantSubscription(ctx context.Context, tenantID string, customerID, subID, priceID, status string, periodEnd time.Time, cancelAtPeriodEnd bool) error

	// GetTenantSubscription returns a tenant's Stripe subscription info.
	GetTenantSubscription(ctx context.Context, tenantID string) (*TenantSubscription, error)

	// ResolveTenantByStripeCustomer resolves a tenant ID by their Stripe customer ID.
	ResolveTenantByStripeCustomer(ctx context.Context, customerID string) (string, error)

	// ResolveTenantByStripeSubscription resolves a tenant ID by their Stripe subscription ID.
	ResolveTenantByStripeSubscription(ctx context.Context, subID string) (string, error)

	// UpdateTenantPlan updates tenants.plan_id.
	UpdateTenantPlan(ctx context.Context, tenantID, planID string) error

	// CreateTransaction inserts a billing/invoice transaction log.
	CreateTransaction(ctx context.Context, tx *Transaction) error

	// ListTransactions returns a tenant's payment history, newest first.
	ListTransactions(ctx context.Context, tenantID string) ([]*Transaction, error)
}
