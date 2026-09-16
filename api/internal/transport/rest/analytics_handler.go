package rest

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/analytics"
	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// AnalyticsHandler handles REST routes for observability aggregates and prompt management.
type AnalyticsHandler struct {
	tenants audit.TenantStore
	service *analytics.Service
	logger  zerolog.Logger
}

// NewAnalyticsHandler constructs the analytics REST controller.
func NewAnalyticsHandler(tenants audit.TenantStore, service *analytics.Service, logger zerolog.Logger) *AnalyticsHandler {
	return &AnalyticsHandler{
		tenants: tenants,
		service: service,
		logger:  logger.With().Str("component", "analytics_http").Logger(),
	}
}

// Mount registers the routes with the provided chi.Router.
// Routes are registered at their full paths (not inside r.Route("/v1/me", ...))
// to avoid shadowing the /v1/me sub-router already mounted by MeHandler.
func (h *AnalyticsHandler) Mount(r chi.Router) {
	r.Get("/v1/me/analytics/llm", h.llmMetrics)
	r.Get("/v1/me/analytics/tools", h.toolMetrics)
	r.Get("/v1/me/analytics/errors", h.errorMetrics)
	r.Get("/v1/me/analytics/sessions", h.listSessions)
	r.Get("/v1/me/analytics/sessions/{id}", h.getSession)

	// Prompt Catalog Management
	r.Get("/v1/me/prompts", h.listPrompts)
	r.Post("/v1/me/prompts", h.createPrompt)
	r.Get("/v1/me/prompts/{id}", h.getPrompt)
}

// resolveTenant resolves the active tenant_id via JWT principal.
func (h *AnalyticsHandler) resolveTenant(r *http.Request) (*audit.Tenant, error) {
	p, _ := auth.PrincipalFromContext(r.Context())
	if p == nil || p.OrgID == "" {
		return nil, apperrors.InvalidInput("no active organization")
	}
	return h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	to := time.Now().UTC()
	from := to.Add(-30 * 24 * time.Hour) // default to last 30 days

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t.UTC()
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t.UTC()
		}
	}
	return from, to
}

func (h *AnalyticsHandler) llmMetrics(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	from, to := parseTimeRange(r)
	metrics, err := h.service.GetLLMMetrics(r.Context(), tenant.ID, from, to)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

func (h *AnalyticsHandler) toolMetrics(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	from, to := parseTimeRange(r)
	metrics, err := h.service.GetToolMetrics(r.Context(), tenant.ID, from, to)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

func (h *AnalyticsHandler) errorMetrics(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	from, to := parseTimeRange(r)
	metrics, err := h.service.GetErrorMetrics(r.Context(), tenant.ID, from, to)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

func (h *AnalyticsHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	limit := 20
	offset := 0

	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil && o >= 0 {
			offset = o
		}
	}

	sessions, err := h.service.ListSessions(r.Context(), tenant.ID, limit, offset)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, sessions)
}

func (h *AnalyticsHandler) getSession(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeAppError(w, apperrors.InvalidInput("session ID is required"))
		return
	}

	detail, err := h.service.GetSessionDetail(r.Context(), tenant.ID, sessionID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// ─── Prompts CRUD Handlers ──────────────────────────────────

func (h *AnalyticsHandler) listPrompts(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	prompts, err := h.service.ListPrompts(r.Context(), tenant.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, prompts)
}

type createPromptRequest struct {
	Name       string            `json:"name"`
	Template   string            `json:"template"`
	Variables  []string          `json:"variables"`
	ModelHints []string          `json:"model_hints"`
	Metadata   map[string]string `json:"metadata"`
}

func (h *AnalyticsHandler) createPrompt(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	var req createPromptRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeAppError(w, apperrors.InvalidInput("invalid request body"))
		return
	}

	prompt, err := h.service.CreatePrompt(
		r.Context(),
		tenant.ID,
		req.Name,
		req.Template,
		req.Variables,
		req.ModelHints,
		req.Metadata,
	)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, prompt)
}

func (h *AnalyticsHandler) getPrompt(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeAppError(w, err)
		return
	}

	promptID := chi.URLParam(r, "id")
	if promptID == "" {
		writeAppError(w, apperrors.InvalidInput("prompt ID is required"))
		return
	}

	prompt, err := h.service.GetPrompt(r.Context(), tenant.ID, promptID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, prompt)
}
