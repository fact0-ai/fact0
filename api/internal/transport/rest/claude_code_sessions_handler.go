package rest

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
)

// ClaudeCodeSessionsHandler serves the aggregated Claude Code session views
// that back the dashboard "Coding Agents" page. Read-only; mounted under
// auth.DualAuth so both dashboard JWTs and API keys work.
type ClaudeCodeSessionsHandler struct {
	store  audit.ClaudeCodeSessionStore
	logger zerolog.Logger
}

// NewClaudeCodeSessionsHandler constructs the handler.
func NewClaudeCodeSessionsHandler(store audit.ClaudeCodeSessionStore, logger zerolog.Logger) *ClaudeCodeSessionsHandler {
	return &ClaudeCodeSessionsHandler{
		store:  store,
		logger: logger.With().Str("module", "claude_code_sessions").Logger(),
	}
}

// Mount registers the session routes with full paths.
func (h *ClaudeCodeSessionsHandler) Mount(r chi.Router) {
	r.Get("/api/v1/integrations/claude-code/sessions", h.list)
	r.Get("/api/v1/integrations/claude-code/sessions/{sessionID}", h.get)
}

func (h *ClaudeCodeSessionsHandler) list(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()

	f := audit.ClaudeCodeSessionFilter{
		Cwd:      q.Get("cwd"),
		Page:     atoiOr(q.Get("page"), 1),
		PageSize: atoiOr(q.Get("page_size"), 50),
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be RFC3339")
			return
		}
		f.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
		f.To = t
	}
	f.Normalise()

	sessions, total, err := h.store.ListClaudeCodeSessions(r.Context(), tenantID, f)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if sessions == nil {
		sessions = []*audit.ClaudeCodeSession{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions":  sessions,
		"total":     total,
		"page":      f.Page,
		"page_size": f.PageSize,
	})
}

func (h *ClaudeCodeSessionsHandler) get(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	session, err := h.store.GetClaudeCodeSession(r.Context(), tenantID, chi.URLParam(r, "sessionID"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}
