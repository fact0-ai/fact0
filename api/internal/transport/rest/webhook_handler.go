package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// AuthWebhookHandler processes inbound webhooks from the Next.js
// dashboard (Better Auth). We sign every delivery with HMAC-SHA256
// over `{timestamp}.{body}` using a shared secret - there's no Svix
// or external infra; the Next.js process is the only authorised
// caller.
//
// Subscribed events (emitted from Better Auth's databaseHooks):
//   - organization.deleted  → soft-delete tenant + revoke all keys
//   - organization.updated  → sync display name
//
// Idempotency: every delivery carries a unique `id` field. We claim
// it in processed_webhooks before doing any state change, so retries
// process at most once.
//
// Signature scheme:
//
//	header  X-Fact0-Timestamp : unix seconds
//	header  X-Fact0-Signature : hex(HMAC-SHA256(secret, ts + "." + body))
//
// Timestamps older than 5 minutes are rejected (anti-replay) - same
// freshness window Svix uses.
type AuthWebhookHandler struct {
	tenants  audit.TenantStore
	keys     audit.KeyStore
	webhooks audit.WebhookStore
	secret   []byte
	logger   zerolog.Logger
}

// NewAuthWebhookHandler returns nil when secret is empty so callers
// can short-circuit mounting the route (404) for local dev without
// the webhook bridge configured.
func NewAuthWebhookHandler(
	tenants audit.TenantStore,
	keys audit.KeyStore,
	webhooks audit.WebhookStore,
	secret string,
	logger zerolog.Logger,
) *AuthWebhookHandler {
	if secret == "" {
		return nil
	}
	return &AuthWebhookHandler{
		tenants:  tenants,
		keys:     keys,
		webhooks: webhooks,
		secret:   []byte(secret),
		logger:   logger.With().Str("component", "auth_webhook").Logger(),
	}
}

// Mount installs POST /webhooks/auth. Authentication is by HMAC inside
// the request (no middleware) - do NOT add any other auth middleware.
func (h *AuthWebhookHandler) Mount(r chi.Router) {
	r.Post("/webhooks/auth", h.handle)
}

const webhookMaxSkew = 5 * time.Minute

type webhookEvent struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type orgData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *AuthWebhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.logger.Warn().Err(err).Msg("reading webhook body")
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	tsStr := r.Header.Get("X-Fact0-Timestamp")
	sig := r.Header.Get("X-Fact0-Signature")
	if tsStr == "" || sig == "" {
		http.Error(w, "missing signature headers", http.StatusUnauthorized)
		return
	}
	tsInt, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid timestamp", http.StatusUnauthorized)
		return
	}
	ts := time.Unix(tsInt, 0)
	if d := time.Since(ts); d > webhookMaxSkew || d < -webhookMaxSkew {
		http.Error(w, "timestamp out of window", http.StatusUnauthorized)
		return
	}

	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(tsStr))
	mac.Write([]byte("."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		h.logger.Warn().Msg("invalid webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var evt webhookEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if evt.ID == "" {
		http.Error(w, "missing event id", http.StatusBadRequest)
		return
	}

	first, err := h.webhooks.MarkProcessed(r.Context(), evt.ID, "better_auth", evt.Type)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if !first {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch evt.Type {
	case "organization.deleted":
		err = h.onOrgDeleted(r, evt.Data)
	case "organization.updated":
		err = h.onOrgUpdated(r, evt.Data)
	default:
		h.logger.Debug().Str("type", evt.Type).Str("id", evt.ID).Msg("ignoring webhook event")
		err = nil
	}

	if err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AuthWebhookHandler) onOrgDeleted(r *http.Request, data json.RawMessage) error {
	var d orgData
	if err := json.Unmarshal(data, &d); err != nil {
		return apperrors.InvalidInput("invalid organization.deleted payload")
	}
	if d.ID == "" {
		return apperrors.InvalidInput("missing organization id")
	}

	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, d.ID)
	if err != nil {
		// Org was never bootstrapped on our side - nothing to delete.
		h.logger.Info().Str("org_id", d.ID).Msg("organization.deleted for unknown tenant")
		return nil
	}

	if err := h.keys.RevokeAllKeys(r.Context(), tenant.ID); err != nil {
		return err
	}
	if err := h.tenants.SoftDeleteTenant(r.Context(), tenant.ID); err != nil {
		return err
	}
	h.logger.Info().
		Str("tenant_id", tenant.ID).
		Str("org_id", d.ID).
		Msg("soft-deleted tenant + revoked all keys")
	return nil
}

func (h *AuthWebhookHandler) onOrgUpdated(r *http.Request, data json.RawMessage) error {
	var d orgData
	if err := json.Unmarshal(data, &d); err != nil {
		return apperrors.InvalidInput("invalid organization.updated payload")
	}
	if d.ID == "" || d.Name == "" {
		return nil
	}

	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, d.ID)
	if err != nil {
		return nil
	}
	if tenant.Name == d.Name {
		return nil
	}
	if err := h.tenants.UpdateTenantName(r.Context(), tenant.ID, d.Name); err != nil {
		return err
	}
	h.logger.Info().
		Str("tenant_id", tenant.ID).
		Str("org_id", d.ID).
		Str("new_name", d.Name).
		Msg("renamed tenant from org update")
	return nil
}
