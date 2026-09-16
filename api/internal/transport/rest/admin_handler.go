package rest

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/notify"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

// AdminHandler serves cross-tenant platform admin APIs.
type AdminHandler struct {
	store       *postgres.AdminStore
	audit       *audit.Service
	emailBridge *notify.EmailBridge
	logger      zerolog.Logger
}

// NewAdminHandler constructs the admin handler.
func NewAdminHandler(store *postgres.AdminStore, auditSvc *audit.Service, emailBridge *notify.EmailBridge, logger zerolog.Logger) *AdminHandler {
	return &AdminHandler{
		store:       store,
		audit:       auditSvc,
		emailBridge: emailBridge,
		logger:      logger.With().Str("component", "admin_http").Logger(),
	}
}

// Mount installs /v1/admin routes. Caller wraps with auth.RequireJWT.
func (h *AdminHandler) Mount(r chi.Router) {
	r.Route("/v1/admin", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequirePlatformRole(auth.PlatformRolesAll...))
			r.Get("/overview", h.overview)
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.RequirePlatformRole(auth.PlatformRolesSupportUp...))
			r.Get("/users", h.listUsers)
			r.Get("/users/{userID}", h.getUser)
			r.Get("/organizations", h.listOrganizations)
			r.Get("/tenants", h.listTenants)
			r.Get("/tenants/{tenantID}", h.getTenant)
			r.Get("/activity", h.listActivity)
			r.Get("/exports", h.listExports)
			r.Get("/billing/transactions", h.listTransactions)
			r.Get("/copilot", h.copilotStats)
			r.Get("/health", h.healthStats)
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.RequirePlatformRole(auth.PlatformRoleFounder))
			r.Patch("/users/{userID}/platform-role", h.setPlatformRole)
			r.Delete("/users/{userID}", h.deleteUser)
			r.Patch("/tenants/{tenantID}/plan", h.setTenantPlan)
			r.Post("/impersonate/start", h.impersonateStart)
			r.Post("/impersonate/end", h.impersonateEnd)
		})
	})
}

func parseAdminListParams(r *http.Request) postgres.ListParams {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	ps, _ := strconv.Atoi(q.Get("page_size"))
	return postgres.ListParams{
		Range:    parseAdminTimeRange(q.Get("from"), q.Get("to")),
		Query:    q.Get("q"),
		Plan:     q.Get("plan"),
		Active:   q.Get("active"),
		Page:     page,
		PageSize: ps,
	}
}

func parseAdminTimeRange(from, to string) postgres.TimeRange {
	now := time.Now().UTC()
	tr := postgres.TimeRange{To: now}
	if to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			tr.To = t
		}
	}
	switch from {
	case "24h", "1d":
		tr.From = now.Add(-24 * time.Hour)
	case "7d":
		tr.From = now.Add(-7 * 24 * time.Hour)
	case "30d":
		tr.From = now.Add(-30 * 24 * time.Hour)
	case "90d":
		tr.From = now.Add(-90 * 24 * time.Hour)
	case "all", "":
		tr.From = time.Time{}
	default:
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			tr.From = t
		}
	}
	return tr
}

func (h *AdminHandler) overview(w http.ResponseWriter, r *http.Request) {
	tr := parseAdminTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	stats, err := h.store.GetOverview(r.Context(), tr)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *AdminHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	p := parseAdminListParams(r)
	rows, total, err := h.store.ListUsers(r.Context(), p)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": rows, "total": total, "page": pageOr1(p.Page), "page_size": pageSizeOr50(p.PageSize),
	})
}

func (h *AdminHandler) getUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	detail, err := h.store.GetUserDetail(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *AdminHandler) listOrganizations(w http.ResponseWriter, r *http.Request) {
	p := parseAdminListParams(r)
	rows, total, err := h.store.ListOrganizations(r.Context(), p)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"organizations": rows, "total": total, "page": pageOr1(p.Page), "page_size": pageSizeOr50(p.PageSize),
	})
}

func (h *AdminHandler) listTenants(w http.ResponseWriter, r *http.Request) {
	p := parseAdminListParams(r)
	rows, total, err := h.store.ListTenants(r.Context(), p)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tenants": rows, "total": total, "page": pageOr1(p.Page), "page_size": pageSizeOr50(p.PageSize),
	})
}

func (h *AdminHandler) getTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "tenantID")
	row, err := h.store.GetTenantDetail(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	h.logPlatformAction(r, id, "platform.admin.view_tenant", map[string]interface{}{"tenant_id": id})
	writeJSON(w, http.StatusOK, row)
}

func (h *AdminHandler) listActivity(w http.ResponseWriter, r *http.Request) {
	p := parseAdminListParams(r)
	q := r.URL.Query()
	events, total, err := h.store.ListActivity(r.Context(), p,
		q.Get("tenant_id"), q.Get("action"), q.Get("actor_type"), q.Get("outcome"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events, "total": total, "page": pageOr1(p.Page), "page_size": pageSizeOr50(p.PageSize),
	})
}

func (h *AdminHandler) listExports(w http.ResponseWriter, r *http.Request) {
	p := parseAdminListParams(r)
	events, total, err := h.store.ListExports(r.Context(), p)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exports": events, "total": total, "page": pageOr1(p.Page), "page_size": pageSizeOr50(p.PageSize),
	})
}

func (h *AdminHandler) copilotStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.CopilotStats(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *AdminHandler) healthStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.HealthStats(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type setPlatformRoleBody struct {
	Role *string `json:"role"` // founder|support|viewer|null
}

func (h *AdminHandler) deleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	p, _ := auth.PrincipalFromContext(r.Context())
	actorID := ""
	if p != nil {
		actorID = p.UserID
	}
	if err := h.store.HardDeleteUser(r.Context(), actorID, userID); err != nil {
		writeAppError(w, err)
		return
	}
	h.logPlatformAction(r, userID, "platform.admin.hard_delete_user", map[string]interface{}{
		"target_user_id": userID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) setPlatformRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	var body setPlatformRoleBody
	if err := requestJSONDecoder(r).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	role := ""
	if body.Role != nil {
		role = *body.Role
		switch role {
		case auth.PlatformRoleFounder, auth.PlatformRoleSupport, auth.PlatformRoleViewer, "":
		default:
			writeError(w, http.StatusBadRequest, "role must be founder, support, viewer, or null")
			return
		}
	}
	if err := h.store.SetPlatformRole(r.Context(), userID, role); err != nil {
		writeAppError(w, err)
		return
	}
	h.logPlatformAction(r, userID, "platform.admin.set_platform_role", map[string]interface{}{
		"target_user_id": userID, "role": role,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type setPlanBody struct {
	PlanID string `json:"plan_id"`
}

func (h *AdminHandler) setTenantPlan(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	var body setPlanBody
	if err := requestJSONDecoder(r).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.PlanID == "" {
		writeError(w, http.StatusBadRequest, "plan_id required")
		return
	}

	if body.PlanID == "pro_1month" {
		oneMonthLater := time.Now().AddDate(0, 1, 0)
		if err := h.store.SetTenantPlanPromo(r.Context(), tenantID, "pro", oneMonthLater); err != nil {
			writeAppError(w, err)
			return
		}
		h.logPlatformAction(r, tenantID, "platform.admin.change_plan_promo", map[string]interface{}{
			"tenant_id": tenantID, "plan_id": "pro", "expires_at": oneMonthLater,
		})

		// Look up tenant owner email and name, then send promotional email asynchronously
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			email, ownerName, tenantName, err := h.store.GetTenantOwnerInfo(bgCtx, tenantID)
			if err != nil {
				h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("failed to load owner info for promo email")
				return
			}
			if email == "" {
				h.logger.Warn().Str("tenant_id", tenantID).Msg("no owner email found, skipping promo email")
				return
			}

			err = h.emailBridge.Send(bgCtx, "promo-upgrade", email, map[string]any{
				"workspaceName": tenantName,
				"userName":      ownerName,
				"expiresAt":     oneMonthLater.Format("January 2, 2006"),
			})
			if err != nil {
				h.logger.Error().Err(err).Str("tenant_id", tenantID).Str("email", email).Msg("failed to send promo-upgrade email")
			}
		}()

	} else {
		if err := h.store.SetTenantPlan(r.Context(), tenantID, body.PlanID); err != nil {
			writeAppError(w, err)
			return
		}
		h.logPlatformAction(r, tenantID, "platform.admin.change_plan", map[string]interface{}{
			"tenant_id": tenantID, "plan_id": body.PlanID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type impersonateBody struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason"`
}

func (h *AdminHandler) impersonateStart(w http.ResponseWriter, r *http.Request) {
	var body impersonateBody
	if err := requestJSONDecoder(r).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.UserID == "" || body.Reason == "" {
		writeError(w, http.StatusBadRequest, "user_id and reason required")
		return
	}
	h.logPlatformAction(r, body.UserID, "platform.admin.impersonate_start", map[string]interface{}{
		"target_user_id": body.UserID, "reason": body.Reason,
	})
	// Cookie is set by the Next.js route handler; return target for client redirect.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"user_id":     body.UserID,
		"read_only":   true,
		"expires_min": 30,
	})
}

func (h *AdminHandler) impersonateEnd(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	if p != nil {
		h.logPlatformAction(r, p.UserID, "platform.admin.impersonate_end", nil)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AdminHandler) logPlatformAction(r *http.Request, resourceID, action string, meta map[string]interface{}) {
	// disabled: system/platform admin events must not be added to the user audit logs
}

func pageOr1(p int) int {
	if p < 1 {
		return 1
	}
	return p
}

func pageSizeOr50(ps int) int {
	if ps < 1 || ps > 200 {
		return 50
	}
	return ps
}

func (h *AdminHandler) listTransactions(w http.ResponseWriter, r *http.Request) {
	p := parseAdminListParams(r)
	rows, total, err := h.store.ListTransactions(r.Context(), p)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": rows,
		"total":        total,
		"page":         pageOr1(p.Page),
		"page_size":    pageSizeOr50(p.PageSize),
	})
}
