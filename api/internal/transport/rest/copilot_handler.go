package rest

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/copilot"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

var errTenantNotBootstrapped = errors.New("tenant not bootstrapped")

// CopilotHandler exposes JWT-authenticated copilot session + audit routes.
type CopilotHandler struct {
	svc     *copilot.Service
	tenants audit.TenantStore
	logger  zerolog.Logger
}

// NewCopilotHandler constructs the handler.
func NewCopilotHandler(svc *copilot.Service, tenants audit.TenantStore, logger zerolog.Logger) *CopilotHandler {
	return &CopilotHandler{
		svc:     svc,
		tenants: tenants,
		logger:  logger.With().Str("component", "copilot_http").Logger(),
	}
}

// Mount installs /v1/me/copilot routes. Caller wraps r with RequireJWT + RequireOrg.
func (h *CopilotHandler) Mount(r chi.Router) {
	r.Route("/v1/me/copilot", func(r chi.Router) {
		r.Post("/audit", h.logAudit) // compat alias
		r.Post("/activity", h.logAudit)
		r.Get("/quota", h.getQuota)
		r.Post("/quota/increment", h.incrementQuota)
		r.Post("/sessions", h.createSession)
		r.Get("/sessions", h.listSessions)
		r.Get("/sessions/{sessionID}", h.getSession)
		r.Get("/sessions/{sessionID}/messages", h.listMessages)
		r.Post("/sessions/{sessionID}/messages", h.appendMessages)
	})
}

type appendMessagesRequest struct {
	Messages []copilot.MessageInput `json:"messages"`
}

func (h *CopilotHandler) createSession(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	sess, err := h.svc.CreateSession(r.Context(), p, tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (h *CopilotHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	sessions, err := h.svc.ListSessions(r.Context(), p, tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if sessions == nil {
		sessions = []*copilot.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

func (h *CopilotHandler) getSession(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	sess, err := h.svc.GetSession(r.Context(), p, tenantID, chi.URLParam(r, "sessionID"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *CopilotHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	msgs, err := h.svc.ListMessages(r.Context(), p, tenantID, chi.URLParam(r, "sessionID"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	if msgs == nil {
		msgs = []*copilot.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"messages": msgs})
}

func (h *CopilotHandler) appendMessages(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	var body appendMessagesRequest
	if err := requestJSONDecoder(r).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	if err := h.svc.AppendMessages(r.Context(), p, tenantID, sessionID, body.Messages); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *CopilotHandler) logAudit(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	var in copilot.AuditInput
	if err := requestJSONDecoder(r).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.LogAudit(r.Context(), p, tenantID, in); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *CopilotHandler) resolve(r *http.Request) (copilot.Principal, string, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil || principal.OrgID == "" {
		return copilot.Principal{}, "", apperrors.InvalidInput("no active organization")
	}
	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, principal.OrgID)
	if err != nil {
		var appErr *apperrors.Error
		if errors.As(err, &appErr) && appErr.Code == apperrors.CodeNotFound {
			return copilot.Principal{}, "", errTenantNotBootstrapped
		}
		return copilot.Principal{}, "", err
	}
	return copilot.Principal{
		UserID: principal.UserID,
		OrgID:  principal.OrgID,
	}, tenant.ID, nil
}

func (h *CopilotHandler) getQuota(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	quota, err := h.svc.CheckQuota(r.Context(), p, tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, quota)
}

func (h *CopilotHandler) incrementQuota(w http.ResponseWriter, r *http.Request) {
	p, tenantID, err := h.resolve(r)
	if err != nil {
		if errors.Is(err, errTenantNotBootstrapped) {
			writeError(w, http.StatusPreconditionFailed, "tenant not bootstrapped")
			return
		}
		writeAppError(w, err)
		return
	}
	newCount, err := h.svc.IncrementUsage(r.Context(), p, tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"used": newCount})
}
