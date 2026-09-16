package rest

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/notify"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"github.com/fact0-ai/fact0/internal/share"
)

// ShareHandler manages dashboard share links and tenant alert settings.
type ShareHandler struct {
	links   *share.Store
	tenants audit.TenantStore
	alerter *notify.Alerter
	logger  zerolog.Logger
}

// NewShareHandler constructs ShareHandler.
func NewShareHandler(links *share.Store, tenants audit.TenantStore, alerter *notify.Alerter, logger zerolog.Logger) *ShareHandler {
	return &ShareHandler{
		links:   links,
		tenants: tenants,
		alerter: alerter,
		logger:  logger.With().Str("component", "share_http").Logger(),
	}
}

// Mount installs /v1/me/share-links and settings routes. Caller wraps with JWT + org.
// Routes are registered at full paths - do not nest another /v1/me Route here;
// MeHandler already owns that prefix on the same router.
func (h *ShareHandler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.AdminRoles...))
		r.Get("/v1/me/share-links", h.listLinks)
		r.Post("/v1/me/share-links", h.createLink)
		r.Delete("/v1/me/share-links/{id}", h.revokeLink)
		r.Get("/v1/me/settings/alerts", h.getAlertSettings)
		r.Put("/v1/me/settings/alerts", h.putAlertSettings)
		r.Post("/v1/me/settings/alerts/test", h.testAlertWebhook)
	})
}

type createShareRequest struct {
	Label      string  `json:"label"`
	ExpiresIn  string  `json:"expires_in"` // 7d, 30d, 90d
	FilterFrom *string `json:"filter_from,omitempty"`
	FilterTo   *string `json:"filter_to,omitempty"`
}

func (h *ShareHandler) createLink(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.OrgID == "" {
		writeError(w, http.StatusPreconditionFailed, "organization required")
		return
	}
	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	var in createShareRequest
	if err := requestJSONDecoder(r).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	planId := "free"
	if resolver, ok := h.tenants.(planResolver); ok {
		plan, err := resolver.GetTenantPlan(r.Context(), tenant.ID)
		if err == nil && plan != nil {
			planId = plan.ID
		}
	}
	if planId == "free" {
		existingLinks, err := h.links.List(r.Context(), tenant.ID)
		if err == nil {
			activeCount := 0
			now := time.Now().UTC()
			for _, l := range existingLinks {
				if l.RevokedAt == nil && l.ExpiresAt.After(now) {
					activeCount++
				}
			}
			if activeCount >= 1 {
				writeError(w, http.StatusForbidden, "Free tier is limited to 1 active share link - please upgrade to Pro")
				return
			}
		}
	}

	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)
	switch in.ExpiresIn {
	case "7d", "7":
		expiresAt = time.Now().UTC().Add(7 * 24 * time.Hour)
	case "90d", "90":
		expiresAt = time.Now().UTC().Add(90 * 24 * time.Hour)
	case "30d", "30", "":
		// default 30d
	}

	var filterFrom, filterTo *time.Time
	if in.FilterFrom != nil && *in.FilterFrom != "" {
		t, err := time.Parse(time.RFC3339, *in.FilterFrom)
		if err != nil {
			writeError(w, http.StatusBadRequest, "filter_from must be RFC3339")
			return
		}
		filterFrom = &t
	}
	if in.FilterTo != nil && *in.FilterTo != "" {
		t, err := time.Parse(time.RFC3339, *in.FilterTo)
		if err != nil {
			writeError(w, http.StatusBadRequest, "filter_to must be RFC3339")
			return
		}
		filterTo = &t
	}

	link, err := h.links.Create(r.Context(), tenant.ID, p.UserID, share.CreateInput{
		Label:      in.Label,
		FilterFrom: filterFrom,
		FilterTo:   filterTo,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

func (h *ShareHandler) listLinks(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	links, err := h.links.List(r.Context(), tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (h *ShareHandler) revokeLink(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.links.Revoke(r.Context(), tenantID, chi.URLParam(r, "id")); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ShareHandler) getAlertSettings(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	settings, err := h.links.GetSettings(r.Context(), tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *ShareHandler) putAlertSettings(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var incoming share.TenantSettings
	if err := requestJSONDecoder(r).Decode(&incoming); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if incoming.AlertWebhookURL != "" {
		if resolver, ok := h.tenants.(planResolver); ok {
			plan, err := resolver.GetTenantPlan(r.Context(), tenantID)
			if err == nil && plan != nil && plan.ID == "free" {
				writeError(w, http.StatusForbidden, "Real-time webhook alerts require a Pro or Team plan. Please upgrade your workspace.")
				return
			}
		}
	}

	existing, err := h.links.GetSettings(r.Context(), tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	existing.AlertWebhookURL = incoming.AlertWebhookURL
	existing.AlertEmail = incoming.AlertEmail
	if err := h.links.UpdateSettings(r.Context(), tenantID, existing); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *ShareHandler) testAlertWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	settings, err := h.links.GetSettings(r.Context(), tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if settings.AlertWebhookURL == "" && settings.AlertEmail == "" {
		writeError(w, http.StatusBadRequest, "alert_webhook_url or alert_email required")
		return
	}

	if settings.AlertWebhookURL != "" {
		if resolver, ok := h.tenants.(planResolver); ok {
			plan, err := resolver.GetTenantPlan(r.Context(), tenantID)
			if err == nil && plan != nil && plan.ID == "free" {
				writeError(w, http.StatusForbidden, "Real-time webhook alerts require a Pro or Team plan. Please upgrade your workspace.")
				return
			}
		}
	}

	out := map[string]string{}
	if settings.AlertWebhookURL != "" {
		payload := notify.ChainBreakPayload{
			Event:    "fact0.test",
			TenantID: tenantID,
			Valid:    true,
		}
		if err := notify.WebhookPOST(r.Context(), settings.AlertWebhookURL, payload); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		out["webhook"] = "sent"
	}

	if settings.AlertEmail != "" && h.alerter != nil {
		workspace := tenantID
		if h.tenants != nil {
			if t, err := h.tenants.GetTenant(r.Context(), tenantID); err == nil && t.Name != "" {
				workspace = t.Name
			}
		}
		if err := h.alerter.TestEmail(r.Context(), tenantID, workspace, settings.AlertEmail); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		out["email"] = "sent"
	}

	writeJSON(w, http.StatusOK, out)
}

func (h *ShareHandler) resolveTenantID(r *http.Request) (string, error) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.OrgID == "" {
		return "", apperrors.InvalidInput("organization required")
	}
	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
	if err != nil {
		return "", err
	}
	return tenant.ID, nil
}

// PublicMeta returns non-sensitive metadata for a share link.
func (h *ShareHandler) PublicMeta(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if parts := strings.SplitN(id, "_", 3); len(parts) == 3 {
		id = parts[0] + "_" + parts[1]
	}
	link, err := h.links.GetByID(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if !link.Valid(time.Now().UTC()) {
		writeError(w, http.StatusGone, "share link expired or revoked")
		return
	}
	tenantName := link.TenantID
	if h.tenants != nil {
		if t, err := h.tenants.GetTenant(r.Context(), link.TenantID); err == nil {
			tenantName = t.Name
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          link.ID,
		"label":       link.Label,
		"tenant_name": tenantName,
		"expires_at":  link.ExpiresAt,
		"filter_from": link.FilterFrom,
		"filter_to":   link.FilterTo,
	})
}
