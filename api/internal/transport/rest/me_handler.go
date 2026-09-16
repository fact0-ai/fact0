package rest

import (
	"bytes"
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// MeHandler wires the JWT-authenticated dashboard surface. The
// dashboard (Better Auth) hits these endpoints with the user's JWT -
// every route here resolves the active org id to a backend tenant_id
// via tenant_external_identities.
//
// Key UX guarantees:
//
//   - POST /v1/me/bootstrap is idempotent AND race-safe. The dashboard
//     calls it on every load; if the tenant/key already exists we
//     return the existing tenant_id but only return a freshly-minted
//     secret if this is the very first call (no keys exist yet).
//     Concurrent callers serialise on a per-external-id advisory lock
//     so racing browser tabs can never produce two orphan keys.
//   - POST /v1/me/sse-ticket mints a 60s single-use token the
//     dashboard uses to open the EventSource stream without ever
//     putting an API key or JWT in the URL.
//   - GET /v1/me/status powers the onboarding checklist and never
//     leaks key material.
//
// Write routes (bootstrap auto-provisions infra; createKey/revokeKey
// manage credentials) are additionally gated on the "admin" or "owner"
// role. A regular member can still view status, list keys (metadata
// only - no hash), and open an SSE stream.
type MeHandler struct {
	ossMode      bool
	tenants      audit.TenantStore
	keys         audit.KeyStore
	stats        audit.StatsStore
	plans        audit.PlanStore
	bootstrapper audit.Bootstrapper
	tickets      *auth.SSETicketStore
	billing      audit.BillingStore
	logger       zerolog.Logger
}

// NewMeHandler constructs the dashboard handler.
func NewMeHandler(
	tenants audit.TenantStore,
	keys audit.KeyStore,
	stats audit.StatsStore,
	plans audit.PlanStore,
	bootstrapper audit.Bootstrapper,
	tickets *auth.SSETicketStore,
	logger zerolog.Logger,
) *MeHandler {
	billing, _ := tenants.(audit.BillingStore)
	return &MeHandler{
		tenants:      tenants,
		keys:         keys,
		stats:        stats,
		plans:        plans,
		bootstrapper: bootstrapper,
		tickets:      tickets,
		billing:      billing,
		logger:       logger.With().Str("component", "me_http").Logger(),
	}
}

func (h *MeHandler) SetOSSMode(enabled bool) {
	h.ossMode = enabled
	if enabled {
		h.billing = nil
	}
}

// Mount installs /v1/me routes on r. Caller wraps r with
// auth.RequireJWT + auth.RequireOrg.
func (h *MeHandler) Mount(r chi.Router) {
	r.Route("/v1/me", func(r chi.Router) {
		// Any signed-in org member.
		r.Get("/status", h.status)
		r.Get("/keys", h.listKeys)
		r.Get("/plan", h.plan)
		r.Get("/usage", h.usage)
		if !h.ossMode {
			r.Get("/billing/transactions", h.transactions)
			r.Post("/billing/checkout", h.checkout)
			r.Post("/billing/portal", h.portal)
			r.Post("/billing/cancel", h.cancelSubscription)
		}
		r.Get("/instrumentation", h.instrumentation)
		r.Post("/sse-ticket", h.issueSSETicket)

		// Admin-only writes.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.AdminRoles...))
			r.Post("/bootstrap", h.bootstrap)
			r.Post("/keys", h.createKey)
			r.Delete("/keys/{keyID}", h.revokeKey)
		})
	})
}

// ─── Response types ───────────────────────────────────────

type bootstrapResponse struct {
	Tenant      *audit.Tenant `json:"tenant"`
	Key         string        `json:"key,omitempty"` // only present on first provision
	KeyID       string        `json:"key_id"`
	Scope       audit.Scope   `json:"scope"`
	Provisioned bool          `json:"provisioned"`
}

type statusResponse struct {
	TenantID string `json:"tenant_id"`
	HasKeys  bool   `json:"has_keys"`
	// HasActivity reports whether the tenant has produced any
	// signal we'd consider "alive". Today that's strictly audit
	// events; the dashboard uses it to exit Setup Mode.
	//
	// TODO(phase-2b): include execution-telemetry counts once the
	// telemetry pipeline is tenant-scoped. Until then, sending
	// telemetry without an audit event will leave new users stuck
	// in Setup Mode even though they're clearly integrated.
	HasActivity    bool  `json:"has_activity"`
	EventCount     int64 `json:"event_count"`
	ChainHeight    int64 `json:"chain_height"`
	OnboardingStep int   `json:"onboarding_step"` // 1..4, 0 == done
}

type keySummary struct {
	ID        string      `json:"id"`
	Scope     audit.Scope `json:"scope"`
	Label     string      `json:"label,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	Revoked   bool        `json:"revoked"`
	RevokedAt *time.Time  `json:"revoked_at,omitempty"`
}

type listKeysResponse struct {
	Keys []keySummary `json:"keys"`
}

type createKeyBody struct {
	Scope audit.Scope `json:"scope"`
	Label string      `json:"label,omitempty"`
}

type createKeyResp struct {
	ID        string      `json:"id"`
	Key       string      `json:"key"` // raw - shown once
	Scope     audit.Scope `json:"scope"`
	Label     string      `json:"label,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// ─── Handlers ─────────────────────────────────────────────

// bootstrap is the "make sure I have a tenant and at least one key"
// endpoint the dashboard hits on every load. Safe to call repeatedly
// and race-safe across concurrent browser tabs (BootstrapTenant runs
// the whole upsert under a per-external-identity advisory lock).
func (h *MeHandler) bootstrap(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	if p == nil || p.OrgID == "" {
		writeError(w, http.StatusPreconditionFailed, "no active organization")
		return
	}

	keyID := audit.NewKeyID()
	rawToken, hashStr, err := auth.GenerateKey(keyID)
	if err != nil {
		writeAppError(w, apperrors.Internal("generating key", err))
		return
	}
	tenant, active, provisioned, err := h.bootstrapper.BootstrapTenant(r.Context(), audit.BootstrapInput{
		Provider:   audit.ProviderBetterAuthOrg,
		ExternalID: p.OrgID,
		TenantID:   audit.NewTenantID(),
		TenantName: p.OrgID,
		KeyCandidate: &audit.APIKey{
			ID:    keyID,
			Hash:  hashStr,
			Scope: audit.ScopeWrite,
			Label: "Default (auto-provisioned)",
		},
	})
	if err != nil {
		writeAppError(w, err)
		return
	}

	resp := bootstrapResponse{
		Tenant:      tenant,
		KeyID:       active.ID,
		Scope:       active.Scope,
		Provisioned: provisioned,
	}
	status := http.StatusOK
	if provisioned {
		resp.Key = rawToken
		status = http.StatusCreated
		h.logger.Info().
			Str("tenant_id", tenant.ID).
			Str("org_id", p.OrgID).
			Str("user_id", p.UserID).
			Msg("auto-provisioned tenant + default key")
	}
	writeJSON(w, status, resp)
}

func (h *MeHandler) status(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())

	// Tenant may not be provisioned yet - that's a valid "Setup Mode"
	// state. Return zeros without erroring so the onboarding checklist
	// can render its step-1 copy.
	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, statusResponse{
				TenantID:       "",
				HasKeys:        false,
				HasActivity:    false,
				OnboardingStep: 1,
			})
			return
		}
		writeAppError(w, err)
		return
	}

	keys, err := h.keys.ListKeys(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	hasKeys := firstActiveKey(keys) != nil

	count, err := h.stats.CountEvents(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	_, _, err = h.stats.ChainHead(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	step := 0
	switch {
	case !hasKeys:
		step = 2 // tenant exists, need a key
	case count == 0:
		step = 4 // SDK installed assumption; waiting for first event
	}

	writeJSON(w, http.StatusOK, statusResponse{
		TenantID:       tenant.ID,
		HasKeys:        hasKeys,
		HasActivity:    count > 0,
		EventCount:     count,
		ChainHeight:    count,
		OnboardingStep: step,
	})
}

func (h *MeHandler) listKeys(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	keys, err := h.keys.ListKeys(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	out := listKeysResponse{Keys: make([]keySummary, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, keySummary{
			ID:        k.ID,
			Scope:     k.Scope,
			Label:     k.Label,
			CreatedAt: k.CreatedAt,
			Revoked:   k.RevokedAt != nil,
			RevokedAt: k.RevokedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MeHandler) createKey(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	var body createKeyBody
	_ = requestJSONDecoder(r).Decode(&body)
	if !body.Scope.IsValid() {
		body.Scope = audit.ScopeWrite
	}

	rawToken, k, err := h.issueKey(r.Context(), tenant.ID, body.Scope, body.Label)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createKeyResp{
		ID:        k.ID,
		Key:       rawToken,
		Scope:     k.Scope,
		Label:     k.Label,
		CreatedAt: k.CreatedAt,
	})
}

func (h *MeHandler) revokeKey(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	keyID := chi.URLParam(r, "keyID")
	if err := h.keys.RevokeKey(r.Context(), tenant.ID, keyID); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sseTicketResponse struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// issueSSETicket exchanges the request's Clerk JWT for a short-lived
// single-use ticket the browser uses to open EventSource without
// putting credential material in the URL.
//
// The ticket is bound to the resolved tenant id (read scope only) and
// expires in 60 seconds. Reconnecting EventSources MUST request a
// fresh ticket - the consume path is LoadAndDelete.
func (h *MeHandler) issueSSETicket(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusServiceUnavailable, "sse tickets unavailable")
		return
	}
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	const ttl = 60 * time.Second
	tok, err := h.tickets.Issue(tenant.ID, audit.ScopeRead, ttl)
	if err != nil {
		writeAppError(w, apperrors.Internal("issuing sse ticket", err))
		return
	}
	writeJSON(w, http.StatusOK, sseTicketResponse{
		Ticket:    tok,
		ExpiresIn: int(ttl.Seconds()),
	})
}

// planResponse describes the tenant's billing tier. retention_days nil
// signals an unlimited plan ("Custom"); the dashboard renders that as
// the "Let's Talk" card.
type planResponse struct {
	Plan              *audit.Plan `json:"plan"`
	RetentionDays     *int        `json:"retention_days,omitempty"`
	MonthlyEventLimit *int64      `json:"monthly_event_limit,omitempty"`
}

// plan returns the active billing plan for the resolved tenant.
// Tenants that haven't bootstrapped yet get the default ("free") plan
// shape so the UI never has to special-case "unprovisioned".
func (h *MeHandler) plan(w http.ResponseWriter, r *http.Request) {
	if h.ossMode {
		writeJSON(w, http.StatusOK, planResponse{Plan: &audit.Plan{ID: "oss", Name: "Self-hosted"}})
		return
	}
	if h.plans == nil {
		writeError(w, http.StatusServiceUnavailable, "plans unavailable")
		return
	}
	tenant, err := h.resolveTenant(r)
	if err != nil {
		if isNotFound(err) {
			// Pre-bootstrap: return synthetic free plan so the billing
			// page renders something instead of 404.
			rd := 30
			ml := int64(30000)
			writeJSON(w, http.StatusOK, planResponse{
				Plan: &audit.Plan{
					ID:                "free",
					Name:              "Free",
					RetentionDays:     &rd,
					MonthlyEventLimit: &ml,
					Description:       "Free tier with 30-day retention.",
				},
				RetentionDays:     &rd,
				MonthlyEventLimit: &ml,
			})
			return
		}
		writeAppError(w, err)
		return
	}
	// Dynamic Self-Healing Synchronization
	if h.billing != nil {
		sub, err := h.billing.GetTenantSubscription(r.Context(), tenant.ID)
		if err == nil && sub != nil && sub.StripeSubscriptionID != nil && *sub.StripeSubscriptionID != "" {
			subID := *sub.StripeSubscriptionID
			tenantID := tenant.ID
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				razorpayPlanID := h.syncRazorpayTenantSubscription(ctx, tenantID, subID)
				if razorpayPlanID != "" {
					currentPlan, err := h.plans.GetTenantPlan(ctx, tenantID)
					if err == nil && currentPlan != nil && currentPlan.ID != razorpayPlanID {
						h.logger.Info().
							Str("tenant_id", tenantID).
							Str("old_plan", currentPlan.ID).
							Str("new_plan", razorpayPlanID).
							Msg("background self-healing synced plan update")
						_ = h.billing.UpdateTenantPlan(ctx, tenantID, razorpayPlanID)
					}
				}
			}()
		}
	}

	p, err := h.plans.GetTenantPlan(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, planResponse{
		Plan:              p,
		RetentionDays:     p.RetentionDays,
		MonthlyEventLimit: p.MonthlyEventLimit,
	})
}

// usageResponse reports rolling 30-day counters used by the billing page.
type usageResponse struct {
	WindowDays           int      `json:"window_days"`
	AuditEvents          int64    `json:"audit_events"`
	AuditEventsThisMonth int64    `json:"audit_events_this_month"`
	MonthlyEventLimit    *int64   `json:"monthly_event_limit,omitempty"`
	UsagePct             *float64 `json:"usage_pct,omitempty"`
	ExecutionsTotal      int64    `json:"executions_total"`
	SpansTotal           int64    `json:"spans_total"`
	TenantScoped         bool     `json:"tenant_scoped"`
}

// usage returns 30-day rolling counts for the billing page.
func (h *MeHandler) usage(w http.ResponseWriter, r *http.Request) {
	const window = 30 * 24 * time.Hour
	since := time.Now().UTC().Add(-window)

	var auditCount int64
	var monthCount int64
	var monthlyLimit *int64
	var tenantID string
	tenant, err := h.resolveTenant(r)
	if err == nil {
		tenantID = tenant.ID
		auditCount, err = h.stats.CountEventsSince(r.Context(), tenantID, since)
		if err != nil {
			writeAppError(w, err)
			return
		}
		monthCount, err = h.stats.CountEventsThisMonth(r.Context(), tenantID)
		if err != nil {
			writeAppError(w, err)
			return
		}
		if h.plans != nil {
			if p, perr := h.plans.GetTenantPlan(r.Context(), tenantID); perr == nil {
				monthlyLimit = p.MonthlyEventLimit
			}
		}
	} else if !isNotFound(err) {
		writeAppError(w, err)
		return
	}

	var execs, spans int64
	if tenantID != "" {
		execs, spans, err = h.stats.TelemetryCountsSince(r.Context(), tenantID, since)
		if err != nil {
			writeAppError(w, err)
			return
		}
	}

	var usagePct *float64
	if monthlyLimit != nil && *monthlyLimit > 0 {
		pct := float64(monthCount) / float64(*monthlyLimit) * 100
		if pct > 100 {
			pct = 100
		}
		usagePct = &pct
	}

	writeJSON(w, http.StatusOK, usageResponse{
		WindowDays:           30,
		AuditEvents:          auditCount,
		AuditEventsThisMonth: monthCount,
		MonthlyEventLimit:    monthlyLimit,
		UsagePct:             usagePct,
		ExecutionsTotal:      execs,
		SpansTotal:           spans,
		TenantScoped:         true,
	})
}

func (h *MeHandler) instrumentation(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	actions, err := h.stats.ListDistinctActions(r.Context(), tenant.ID, since)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"actions": actions,
		"since":   since,
	})
}

// ─── Helpers ──────────────────────────────────────────────

// resolveTenant looks up the backend tenant for the request's JWT
// principal. Returns NotFound when the org has never called bootstrap
// - clients are expected to POST /v1/me/bootstrap first.
func (h *MeHandler) resolveTenant(r *http.Request) (*audit.Tenant, error) {
	p, _ := auth.PrincipalFromContext(r.Context())
	if p == nil || p.OrgID == "" {
		return nil, apperrors.InvalidInput("no active organization")
	}
	return h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
}

func (h *MeHandler) issueKey(ctx context.Context, tenantID string, scope audit.Scope, label string) (string, *audit.APIKey, error) {
	keyID := audit.NewKeyID()
	rawToken, hashStr, err := auth.GenerateKey(keyID)
	if err != nil {
		return "", nil, apperrors.Internal("generating key", err)
	}
	k := &audit.APIKey{
		ID:        keyID,
		TenantID:  tenantID,
		Hash:      hashStr,
		Scope:     scope,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.keys.CreateKey(ctx, k); err != nil {
		return "", nil, err
	}
	return rawToken, k, nil
}

func firstActiveKey(keys []*audit.APIKey) *audit.APIKey {
	for _, k := range keys {
		if k.RevokedAt == nil {
			return k
		}
	}
	return nil
}

func isNotFound(err error) bool {
	var ae *apperrors.Error
	if !goerrors.As(err, &ae) {
		return false
	}
	return ae.Code == apperrors.CodeNotFound
}

// transactions lists user billing transactions.
func (h *MeHandler) transactions(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"transactions": []interface{}{}})
		return
	}

	if h.billing == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"transactions": []interface{}{}})
		return
	}

	txs, err := h.billing.ListTransactions(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if txs == nil {
		txs = []*audit.Transaction{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"transactions": txs})
}

type checkoutRequest struct {
	PlanID string `json:"plan_id"`
}

type checkoutResponse struct {
	OrderID        string `json:"order_id"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
	KeyID          string `json:"key_id"`
	URL            string `json:"url,omitempty"`
}

// checkout provisions a Razorpay Checkout order or subscription for plan upgrades.
func (h *MeHandler) checkout(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	var req checkoutRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	keyID := strings.Trim(os.Getenv("RAZORPAY_KEY_ID"), "\"' ")
	keySecret := strings.Trim(os.Getenv("RAZORPAY_KEY_SECRET"), "\"' ")

	if keyID == "" || keySecret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "RAZORPAY_KEY_ID or RAZORPAY_KEY_SECRET is not configured on the server"})
		return
	}

	var amount int64
	var currency = "USD"
	switch req.PlanID {
	case "pro":
		amount = 7900 // $79.00 USD (in cents)
	case "team":
		amount = 9900 // $99.00 USD (in cents)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid plan %q for checkout", req.PlanID)})
		return
	}

	// Try creating a recurring Razorpay Subscription
	subID, err := createRazorpaySubscription(r.Context(), keyID, keySecret, req.PlanID, amount, currency, tenant.ID)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenant.ID).Msg("failed to create razorpay subscription, falling back to order")

		// Fallback to one-time order if subscription creation failed
		orderID, oerr := createRazorpayOrder(r.Context(), keyID, keySecret, tenant.ID, amount, currency, req.PlanID)
		if oerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("subscription error: %v; order fallback error: %v", err, oerr)})
			return
		}

		if h.billing != nil {
			_ = h.billing.SetTenantSubscription(r.Context(), tenant.ID, "", orderID, req.PlanID, "created", time.Now().AddDate(0, 1, 0), false)
		}

		writeJSON(w, http.StatusOK, checkoutResponse{
			OrderID:  orderID,
			Amount:   amount,
			Currency: currency,
			KeyID:    keyID,
		})
		return
	}

	// Save subscription ID in database
	if h.billing != nil {
		_ = h.billing.SetTenantSubscription(r.Context(), tenant.ID, "", subID, req.PlanID, "created", time.Now().AddDate(0, 1, 0), false)
	}

	writeJSON(w, http.StatusOK, checkoutResponse{
		SubscriptionID: subID,
		Amount:         amount,
		Currency:       currency,
		KeyID:          keyID,
	})
}

func createRazorpaySubscription(ctx context.Context, keyID, keySecret, planID string, amount int64, currency string, tenantID string) (string, error) {
	var razorpayPlanID string
	switch planID {
	case "pro":
		razorpayPlanID = strings.Trim(os.Getenv("RAZORPAY_PLAN_ID_PRO"), "\"' ")
	case "team":
		razorpayPlanID = strings.Trim(os.Getenv("RAZORPAY_PLAN_ID_TEAM"), "\"' ")
	}

	// If no Plan ID is configured, let's dynamically create a recurring plan on Razorpay!
	if razorpayPlanID == "" {
		planName := "Fact0 Pro Plan"
		if planID == "team" {
			planName = "Fact0 Team Plan"
		}

		planURL := "https://api.razorpay.com/v1/plans"
		planPayload := map[string]interface{}{
			"period":   "monthly",
			"interval": 1,
			"item": map[string]interface{}{
				"name":     planName,
				"amount":   amount,
				"currency": currency,
			},
		}

		planBytes, err := json.Marshal(planPayload)
		if err != nil {
			return "", err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", planURL, bytes.NewBuffer(planBytes))
		if err != nil {
			return "", err
		}

		req.SetBasicAuth(keyID, keySecret)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()

		respBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			var planRes struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(respBytes, &planRes); err == nil && planRes.ID != "" {
				razorpayPlanID = planRes.ID
			}
		}

		if razorpayPlanID == "" {
			return "", fmt.Errorf("failed to create dynamic plan on Razorpay: %s", string(respBytes))
		}
	}

	// Create subscription referencing the plan
	subURL := "https://api.razorpay.com/v1/subscriptions"
	subPayload := map[string]interface{}{
		"plan_id":         razorpayPlanID,
		"total_count":     120, // 10 years recurring monthly
		"quantity":        1,
		"customer_notify": 1,
		"notes": map[string]string{
			"tenant_id": tenantID,
			"plan_id":   planID,
		},
	}

	subBytes, err := json.Marshal(subPayload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", subURL, bytes.NewBuffer(subBytes))
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(keyID, keySecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create Razorpay subscription (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if result.ID == "" {
		return "", fmt.Errorf("missing subscription id in response: %s", string(respBytes))
	}

	return result.ID, nil
}

func createRazorpayOrder(ctx context.Context, keyID, keySecret, tenantID string, amount int64, currency string, planID string) (string, error) {
	apiURL := "https://api.razorpay.com/v1/orders"

	payload := map[string]interface{}{
		"amount":   amount,
		"currency": currency,
		"notes": map[string]string{
			"tenant_id": tenantID,
			"plan_id":   planID,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(keyID, keySecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("razorpay order API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if result.ID == "" {
		return "", fmt.Errorf("missing order id in Razorpay response: %s", string(respBytes))
	}

	return result.ID, nil
}

func (h *MeHandler) syncRazorpayTenantSubscription(ctx context.Context, tenantID, subID string) string {
	keyID := strings.Trim(os.Getenv("RAZORPAY_KEY_ID"), "\"' ")
	keySecret := strings.Trim(os.Getenv("RAZORPAY_KEY_SECRET"), "\"' ")

	if keyID == "" || keySecret == "" || subID == "" {
		return ""
	}

	var apiURL string
	if strings.HasPrefix(subID, "sub_") {
		apiURL = fmt.Sprintf("https://api.razorpay.com/v1/subscriptions/%s", subID)
	} else {
		apiURL = fmt.Sprintf("https://api.razorpay.com/v1/orders/%s", subID)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return ""
	}

	req.SetBasicAuth(keyID, keySecret)
	req.Header.Set("accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	bodyBytes, _ := io.ReadAll(resp.Body)

	if strings.HasPrefix(subID, "sub_") {
		var sub struct {
			Status string            `json:"status"`
			Notes  map[string]string `json:"notes"`
		}
		if err := json.Unmarshal(bodyBytes, &sub); err != nil {
			return ""
		}
		if sub.Status == "active" || sub.Status == "authenticated" {
			if sub.Notes != nil {
				if tagPlan, ok := sub.Notes["plan_id"]; ok {
					return tagPlan
				}
			}
		}
		if sub.Status == "cancelled" || sub.Status == "expired" {
			return "free"
		}
	} else {
		var order struct {
			Status string            `json:"status"`
			Notes  map[string]string `json:"notes"`
		}
		if err := json.Unmarshal(bodyBytes, &order); err != nil {
			return ""
		}
		if order.Status == "paid" {
			if order.Notes != nil {
				if tagPlan, ok := order.Notes["plan_id"]; ok {
					return tagPlan
				}
			}
		}
	}

	return ""
}

// portal is used by the frontend to redirect to billing management.
func (h *MeHandler) portal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, checkoutResponse{URL: "/dashboard/settings/billing"})
}

// cancelSubscription downgrades a paid subscription back to the Free plan.
func (h *MeHandler) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if h.billing == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "billing database service is not configured"})
		return
	}

	// Retrieve active subscription to check if there is one to cancel
	sub, err := h.billing.GetTenantSubscription(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if sub == nil || sub.StripeSubscriptionID == nil || *sub.StripeSubscriptionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No active paid subscription found to cancel"})
		return
	}

	// Cancel subscription on Razorpay if it's a live subscription
	if strings.HasPrefix(*sub.StripeSubscriptionID, "sub_") {
		if err := cancelRazorpaySubscription(r.Context(), *sub.StripeSubscriptionID); err != nil {
			h.logger.Error().Err(err).
				Str("tenant_id", tenant.ID).
				Str("sub_id", *sub.StripeSubscriptionID).
				Msg("failed to cancel Razorpay subscription")
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "Failed to cancel subscription with payment provider. Please try again.",
			})
			return
		}
	}

	// Update DB to clear order tracking and set status to cancelled
	err = h.billing.SetTenantSubscription(
		r.Context(),
		tenant.ID,
		"",          // customerID
		"",          // subID (orderID)
		"",          // priceID (planID)
		"cancelled", // status
		time.Now(),  // periodEnd
		false,       // cancelAtPeriodEnd
	)
	if err != nil {
		writeAppError(w, err)
		return
	}

	// Downgrade tenant plan ID in database back to free
	err = h.billing.UpdateTenantPlan(r.Context(), tenant.ID, "free")
	if err != nil {
		writeAppError(w, err)
		return
	}

	h.logger.Info().Str("tenant_id", tenant.ID).Msg("tenant subscription cancelled successfully")

	writeJSON(w, http.StatusOK, map[string]string{"message": "Subscription cancelled successfully"})
}

func cancelRazorpaySubscription(ctx context.Context, subID string) error {
	keyID := strings.Trim(os.Getenv("RAZORPAY_KEY_ID"), "\"' ")
	keySecret := strings.Trim(os.Getenv("RAZORPAY_KEY_SECRET"), "\"' ")

	if keyID == "" || keySecret == "" || subID == "" {
		return fmt.Errorf("billing credentials or subscription ID missing")
	}

	apiURL := fmt.Sprintf("https://api.razorpay.com/v1/subscriptions/%s/cancel", subID)
	payload := map[string]interface{}{
		"cancel_at_cycle_end": 0, // cancel immediately
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	req.SetBasicAuth(keyID, keySecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accept", "application/json")

	// Production and Design Note:
	// We use http.DefaultClient here because no dependency-injected HTTP client is configured
	// for the rest/handler package. While convenient, using http.DefaultClient in production
	// carries trade-offs:
	// 1. Lack of custom timeout tuning (it defaults to no timeout, though we pass a context with timeout).
	// 2. Shared connection pool, which could lead to resource contention if many external calls are made.
	// 3. Harder to mock or intercept in unit/integration testing compared to a custom interface.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("razorpay subscription cancel API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return nil
}
