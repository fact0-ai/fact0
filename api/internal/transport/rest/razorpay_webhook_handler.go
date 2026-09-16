package rest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
)

// RazorpayWebhookHandler processes inbound webhooks from Razorpay.
type RazorpayWebhookHandler struct {
	billingStore  audit.BillingStore
	auditSvc      *audit.Service
	webhookSecret string
	logger        zerolog.Logger
}

// NewRazorpayWebhookHandler creates a new RazorpayWebhookHandler.
func NewRazorpayWebhookHandler(
	billingStore audit.BillingStore,
	auditSvc *audit.Service,
	webhookSecret string,
	logger zerolog.Logger,
) *RazorpayWebhookHandler {
	return &RazorpayWebhookHandler{
		billingStore:  billingStore,
		auditSvc:      auditSvc,
		webhookSecret: webhookSecret,
		logger:        logger.With().Str("component", "razorpay_webhook").Logger(),
	}
}

// Mount installs POST /api/webhooks/razorpay.
func (h *RazorpayWebhookHandler) Mount(r chi.Router) {
	r.Post("/api/webhooks/razorpay", h.handle)
}

type rzpNotes map[string]string

func (n *rzpNotes) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || (len(data) >= 2 && data[0] == '[' && data[len(data)-1] == ']') {
		*n = make(map[string]string)
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*n = m
	return nil
}

// Structs to parse Razorpay webhooks
type razorpayWebhook struct {
	Event     string     `json:"event"`
	Payload   rzpPayload `json:"payload"`
	CreatedAt int64      `json:"created_at"`
}

type rzpPayload struct {
	Payment      rzpPaymentContainer      `json:"payment"`
	Order        rzpOrderContainer        `json:"order"`
	Subscription rzpSubscriptionContainer `json:"subscription"`
}

type rzpPaymentContainer struct {
	Entity rzpPaymentEntity `json:"entity"`
}

type rzpPaymentEntity struct {
	ID        string   `json:"id"`
	Amount    int64    `json:"amount"` // in paise/cents
	Currency  string   `json:"currency"`
	OrderID   string   `json:"order_id"`
	Status    string   `json:"status"`
	Email     string   `json:"email"`
	Notes     rzpNotes `json:"notes"`
	CreatedAt int64    `json:"created_at"`
	ErrorMsg  string   `json:"error_description"`
}

type rzpOrderContainer struct {
	Entity rzpOrderEntity `json:"entity"`
}

type rzpOrderEntity struct {
	ID        string   `json:"id"`
	Amount    int64    `json:"amount"`
	Currency  string   `json:"currency"`
	Receipt   string   `json:"receipt"`
	Status    string   `json:"status"`
	Notes     rzpNotes `json:"notes"`
	CreatedAt int64    `json:"created_at"`
}

type rzpSubscriptionContainer struct {
	Entity rzpSubscriptionEntity `json:"entity"`
}

type rzpSubscriptionEntity struct {
	ID        string   `json:"id"`
	PlanID    string   `json:"plan_id"`
	Status    string   `json:"status"`
	Notes     rzpNotes `json:"notes"`
	CreatedAt int64    `json:"created_at"`
}

func (h *RazorpayWebhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.logger.Warn().Err(err).Msg("reading razorpay webhook body")
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	sigHeader := r.Header.Get("x-razorpay-signature")

	if h.webhookSecret == "" {
		h.logger.Error().Msg("Razorpay webhook secret is not configured")
		http.Error(w, "webhook secret not configured", http.StatusServiceUnavailable)
		return
	}

	if sigHeader == "" {
		h.logger.Warn().Msg("missing x-razorpay-signature header")
		http.Error(w, "missing signature", http.StatusUnauthorized)
		return
	}

	if !verifyRazorpaySignature(body, sigHeader, h.webhookSecret) {
		h.logger.Warn().Msg("invalid razorpay webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var evt razorpayWebhook
	if err := json.Unmarshal(body, &evt); err != nil {
		h.logger.Warn().Err(err).Msg("invalid razorpay webhook json")
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	h.logger.Info().Str("event", evt.Event).Str("payment_id", evt.Payload.Payment.Entity.ID).Msg("processing Razorpay webhook event")

	switch evt.Event {
	case "order.paid", "payment.captured", "subscription.charged", "subscription.activated", "subscription.authenticated":
		err = h.onPaymentSuccess(ctx, evt.Payload)
	case "payment.failed":
		err = h.onPaymentFailed(ctx, evt.Payload)
	default:
		h.logger.Info().Str("event", evt.Event).Msg("unhandled razorpay webhook event")
	}

	if err != nil {
		h.logger.Error().Err(err).Str("event", evt.Event).Msg("failed processing Razorpay event")
	}

	w.WriteHeader(http.StatusOK)
}

func (h *RazorpayWebhookHandler) onPaymentSuccess(ctx context.Context, payload rzpPayload) error {
	tenantID := payload.Order.Entity.Notes["tenant_id"]
	if tenantID == "" {
		tenantID = payload.Payment.Entity.Notes["tenant_id"]
	}
	if tenantID == "" {
		tenantID = payload.Subscription.Entity.Notes["tenant_id"]
	}

	orderID := payload.Payment.Entity.OrderID
	if orderID == "" {
		orderID = payload.Order.Entity.ID
	}
	if orderID == "" {
		orderID = payload.Subscription.Entity.ID
	}

	if tenantID == "" && orderID != "" {
		if tid, err := h.billingStore.ResolveTenantByStripeSubscription(ctx, orderID); err == nil && tid != "" {
			tenantID = tid
		}
	}

	planID := payload.Order.Entity.Notes["plan_id"]
	if planID == "" {
		planID = payload.Payment.Entity.Notes["plan_id"]
	}
	if planID == "" {
		planID = payload.Subscription.Entity.Notes["plan_id"]
	}

	if tenantID == "" {
		h.logger.Warn().
			Str("payment_id", payload.Payment.Entity.ID).
			Str("order_id", orderID).
			Str("subscription_id", payload.Subscription.Entity.ID).
			Msg("razorpay webhook notes.tenant_id is empty")
		return nil
	}

	if planID == "" {
		planID = "pro"
	}

	h.logger.Info().
		Str("tenant_id", tenantID).
		Str("plan_id", planID).
		Str("order_id", orderID).
		Msg("razorpay payment success: updating subscription state")

	periodEnd := time.Now().AddDate(0, 1, 0)
	err := h.billingStore.SetTenantSubscription(
		ctx,
		tenantID,
		tenantID,
		orderID,
		planID,
		"active",
		periodEnd,
		false,
	)
	if err != nil {
		return err
	}

	err = h.billingStore.UpdateTenantPlan(ctx, tenantID, planID)
	if err != nil {
		return err
	}

	createdAt := time.Unix(payload.Payment.Entity.CreatedAt, 0)
	if payload.Payment.Entity.CreatedAt == 0 {
		createdAt = time.Now()
	}

	amountCents := payload.Payment.Entity.Amount
	currency := payload.Payment.Entity.Currency
	if currency == "" {
		currency = "USD"
	}

	tx := &audit.Transaction{
		ID:               payload.Payment.Entity.ID,
		TenantID:         tenantID,
		StripeInvoiceID:  &orderID,
		AmountCents:      amountCents,
		Currency:         currency,
		Status:           "succeeded",
		HostedInvoiceURL: nil,
		InvoicePDF:       nil,
		CreatedAt:        createdAt,
		UpdatedAt:        time.Now(),
	}

	err = h.billingStore.CreateTransaction(ctx, tx)
	if err != nil {
		return err
	}

	return nil
}

func (h *RazorpayWebhookHandler) onPaymentFailed(ctx context.Context, payload rzpPayload) error {
	tenantID := payload.Order.Entity.Notes["tenant_id"]
	if tenantID == "" {
		tenantID = payload.Payment.Entity.Notes["tenant_id"]
	}
	if tenantID == "" {
		tenantID = payload.Subscription.Entity.Notes["tenant_id"]
	}

	orderID := payload.Payment.Entity.OrderID
	if orderID == "" {
		orderID = payload.Order.Entity.ID
	}
	if orderID == "" {
		orderID = payload.Subscription.Entity.ID
	}

	if tenantID == "" && orderID != "" {
		if tid, err := h.billingStore.ResolveTenantByStripeSubscription(ctx, orderID); err == nil && tid != "" {
			tenantID = tid
		}
	}

	if tenantID == "" {
		h.logger.Warn().
			Str("payment_id", payload.Payment.Entity.ID).
			Str("order_id", orderID).
			Str("subscription_id", payload.Subscription.Entity.ID).
			Msg("razorpay webhook notes.tenant_id is empty")
		return nil
	}

	h.logger.Warn().
		Str("tenant_id", tenantID).
		Str("order_id", orderID).
		Msg("razorpay payment failed: recording failure")

	createdAt := time.Unix(payload.Payment.Entity.CreatedAt, 0)
	if payload.Payment.Entity.CreatedAt == 0 {
		createdAt = time.Now()
	}

	amountCents := payload.Payment.Entity.Amount
	currency := payload.Payment.Entity.Currency
	if currency == "" {
		currency = "USD"
	}

	tx := &audit.Transaction{
		ID:               payload.Payment.Entity.ID,
		TenantID:         tenantID,
		StripeInvoiceID:  &orderID,
		AmountCents:      amountCents,
		Currency:         currency,
		Status:           "failed",
		HostedInvoiceURL: nil,
		InvoicePDF:       nil,
		CreatedAt:        createdAt,
		UpdatedAt:        time.Now(),
	}

	err := h.billingStore.CreateTransaction(ctx, tx)
	if err != nil {
		return err
	}

	return nil
}

func verifyRazorpaySignature(body []byte, signature, secret string) bool {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	expectedSignature := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expectedSignature), []byte(signature))
}
