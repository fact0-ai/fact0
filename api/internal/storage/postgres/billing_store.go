package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// SetTenantSubscription updates a tenant's Stripe billing info.
func (s *AuditStore) SetTenantSubscription(ctx context.Context, tenantID string, customerID, subID, priceID, status string, periodEnd time.Time, cancelAtPeriodEnd bool) error {
	_, err := s.writeDB.Exec(ctx, `
		UPDATE tenants
		SET stripe_customer_id = $2,
		    stripe_subscription_id = $3,
		    stripe_price_id = $4,
		    subscription_status = $5,
		    subscription_current_period_end = $6,
		    subscription_cancel_at_period_end = $7
		WHERE id = $1 AND deleted_at IS NULL`,
		tenantID, customerID, nullableString(subID), nullableString(priceID), nullableString(status), periodEnd, cancelAtPeriodEnd,
	)
	if err != nil {
		return apperrors.Internal("setting tenant subscription", err)
	}
	return nil
}

// GetTenantSubscription returns a tenant's Stripe subscription info.
func (s *AuditStore) GetTenantSubscription(ctx context.Context, tenantID string) (*audit.TenantSubscription, error) {
	var sub audit.TenantSubscription
	err := s.readDB.QueryRow(ctx, `
		SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id,
		       subscription_status, subscription_current_period_end, subscription_cancel_at_period_end
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL`,
		tenantID,
	).Scan(
		&sub.TenantID, &sub.StripeCustomerID, &sub.StripeSubscriptionID, &sub.StripePriceID,
		&sub.SubscriptionStatus, &sub.SubscriptionCurrentPeriodEnd, &sub.SubscriptionCancelAtPeriodEnd,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("tenant_subscription", tenantID)
	}
	if err != nil {
		return nil, apperrors.Internal("getting tenant subscription", err)
	}
	return &sub, nil
}

// ResolveTenantByStripeCustomer resolves a tenant ID by their Stripe customer ID.
func (s *AuditStore) ResolveTenantByStripeCustomer(ctx context.Context, customerID string) (string, error) {
	var id string
	err := s.readDB.QueryRow(ctx, `
		SELECT id FROM tenants
		WHERE stripe_customer_id = $1 AND deleted_at IS NULL`,
		customerID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperrors.NotFound("tenant_by_stripe_customer", customerID)
	}
	if err != nil {
		return "", apperrors.Internal("resolving tenant by stripe customer id", err)
	}
	return id, nil
}

// ResolveTenantByStripeSubscription resolves a tenant ID by their Stripe subscription ID.
func (s *AuditStore) ResolveTenantByStripeSubscription(ctx context.Context, subID string) (string, error) {
	var id string
	err := s.readDB.QueryRow(ctx, `
		SELECT id FROM tenants
		WHERE stripe_subscription_id = $1 AND deleted_at IS NULL`,
		subID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperrors.NotFound("tenant_by_stripe_subscription", subID)
	}
	if err != nil {
		return "", apperrors.Internal("resolving tenant by stripe subscription id", err)
	}
	return id, nil
}

// UpdateTenantPlan updates tenants.plan_id.
func (s *AuditStore) UpdateTenantPlan(ctx context.Context, tenantID, planID string) error {
	_, err := s.writeDB.Exec(ctx, `
		UPDATE tenants SET plan_id = $2 WHERE id = $1 AND deleted_at IS NULL`,
		tenantID, planID,
	)
	if err != nil {
		return apperrors.Internal("updating tenant plan", err)
	}
	return nil
}

// CreateTransaction inserts a billing/invoice transaction log.
func (s *AuditStore) CreateTransaction(ctx context.Context, tx *audit.Transaction) error {
	_, err := s.writeDB.Exec(ctx, `
		INSERT INTO transactions (id, tenant_id, stripe_invoice_id, amount_cents, currency, status, hosted_invoice_url, invoice_pdf, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			hosted_invoice_url = EXCLUDED.hosted_invoice_url,
			invoice_pdf = EXCLUDED.invoice_pdf,
			updated_at = NOW()`,
		tx.ID, tx.TenantID, nullableString(strVal(tx.StripeInvoiceID)), tx.AmountCents, tx.Currency, tx.Status, nullableString(strVal(tx.HostedInvoiceURL)), nullableString(strVal(tx.InvoicePDF)), tx.CreatedAt, tx.UpdatedAt,
	)
	if err != nil {
		return apperrors.Internal("creating transaction record", err)
	}
	return nil
}

// ListTransactions returns a tenant's payment history, newest first.
func (s *AuditStore) ListTransactions(ctx context.Context, tenantID string) ([]*audit.Transaction, error) {
	rows, err := s.readDB.Query(ctx, `
		SELECT id, tenant_id, stripe_invoice_id, amount_cents, currency, status, hosted_invoice_url, invoice_pdf, created_at, updated_at
		FROM transactions
		WHERE tenant_id = $1
		ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, apperrors.Internal("listing transactions", err)
	}
	defer rows.Close()

	var out []*audit.Transaction
	for rows.Next() {
		var tx audit.Transaction
		var invoiceID, invoiceURL, pdf *string
		err := rows.Scan(
			&tx.ID, &tx.TenantID, &invoiceID, &tx.AmountCents, &tx.Currency, &tx.Status,
			&invoiceURL, &pdf, &tx.CreatedAt, &tx.UpdatedAt,
		)
		if err != nil {
			return nil, apperrors.Internal("scanning transaction row", err)
		}
		tx.StripeInvoiceID = invoiceID
		tx.HostedInvoiceURL = invoiceURL
		tx.InvoicePDF = pdf
		out = append(out, &tx)
	}
	return out, rows.Err()
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
