package rest

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
)

// auditSink is the narrow audit-append interface (satisfied by
// *audit.Service) so tests can fake it.
type auditSink interface {
	Log(ctx context.Context, tenantID string, in *audit.AuditEvent) (*audit.AuditEvent, error)
}

// orgTenantResolver maps a JWT principal's org to a tenant (satisfied by
// *postgres.AuditStore and any audit.TenantStore).
type orgTenantResolver interface {
	GetTenantByExternalID(ctx context.Context, provider, externalID string) (*audit.Tenant, error)
}

// ClaudeCodePolicyHandler serves the server-managed governance policy: the
// collector fetches it (read-only, API key or JWT via DualAuth) and the
// dashboard reads/writes it (JWT, writes admin-gated). Every write appends a
// claude_code.policy.updated event to the tamper-evident chain.
type ClaudeCodePolicyHandler struct {
	store   audit.ClaudeCodePolicyStore
	tenants orgTenantResolver
	auditor auditSink
	logger  zerolog.Logger
}

// NewClaudeCodePolicyHandler constructs the handler.
func NewClaudeCodePolicyHandler(store audit.ClaudeCodePolicyStore, tenants orgTenantResolver, auditor auditSink, logger zerolog.Logger) *ClaudeCodePolicyHandler {
	return &ClaudeCodePolicyHandler{
		store:   store,
		tenants: tenants,
		auditor: auditor,
		logger:  logger.With().Str("module", "claude_code_policy").Logger(),
	}
}

// MountRead registers the collector-facing fetch endpoint (DualAuth group).
func (h *ClaudeCodePolicyHandler) MountRead(r chi.Router) {
	r.Get("/api/v1/integrations/claude-code/policy", h.fetch)
}

// MountDashboard registers the dashboard endpoints (JWT group).
func (h *ClaudeCodePolicyHandler) MountDashboard(r chi.Router) {
	r.Get("/v1/me/governance/policy", h.get)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.AdminRoles...))
		r.Put("/v1/me/governance/policy", h.put)
	})
}

// fetch serves the collector: cheap polling via ?version= or If-None-Match.
func (h *ClaudeCodePolicyHandler) fetch(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	policy, err := h.store.GetClaudeCodePolicy(r.Context(), tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	etag := fmt.Sprintf(`"v%d"`, policy.Version)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if v := r.URL.Query().Get("version"); v != "" {
		if have, err := strconv.Atoi(v); err == nil && have == policy.Version {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("ETag", etag)
	// updated_by is dashboard detail; the collector doesn't need it.
	policy.UpdatedBy = ""
	writeJSON(w, http.StatusOK, policy)
}

// tenantFromPrincipal resolves the JWT principal's org to a tenant, matching
// the share handler's convention.
func (h *ClaudeCodePolicyHandler) tenantFromPrincipal(w http.ResponseWriter, r *http.Request) (string, *auth.Principal, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.OrgID == "" {
		writeError(w, http.StatusPreconditionFailed, "organization required")
		return "", nil, false
	}
	tenant, err := h.tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
	if err != nil {
		writeAppError(w, err)
		return "", nil, false
	}
	return tenant.ID, p, true
}

func (h *ClaudeCodePolicyHandler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.tenantFromPrincipal(w, r)
	if !ok {
		return
	}
	policy, err := h.store.GetClaudeCodePolicy(r.Context(), tenantID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

type putPolicyRequest struct {
	Version int                          `json:"version"`
	Enforce bool                         `json:"enforce"`
	Rules   []audit.ClaudeCodePolicyRule `json:"rules"`
}

const (
	maxPolicyRules     = 200
	maxRuleMatchChars  = 1024
	maxRuleToolChars   = 128
	maxRuleReasonChars = 512
)

// validatePolicyRules rejects malformed rules before anything is written.
// Both server and collector use Go RE2, so compile parity is exact.
func validatePolicyRules(rules []audit.ClaudeCodePolicyRule) error {
	if len(rules) > maxPolicyRules {
		return fmt.Errorf("too many rules (%d > %d)", len(rules), maxPolicyRules)
	}
	for i, rule := range rules {
		if strings.TrimSpace(rule.Match) == "" {
			return fmt.Errorf("rule %d: match is required", i)
		}
		if len(rule.Match) > maxRuleMatchChars {
			return fmt.Errorf("rule %d: match exceeds %d chars", i, maxRuleMatchChars)
		}
		if _, err := regexp.Compile(rule.Match); err != nil {
			return fmt.Errorf("rule %d: invalid regexp: %v", i, err)
		}
		if !strings.EqualFold(rule.Action, "deny") {
			return fmt.Errorf("rule %d: action must be \"deny\"", i)
		}
		if len(rule.Tool) > maxRuleToolChars {
			return fmt.Errorf("rule %d: tool exceeds %d chars", i, maxRuleToolChars)
		}
		if len(rule.Reason) > maxRuleReasonChars {
			return fmt.Errorf("rule %d: reason exceeds %d chars", i, maxRuleReasonChars)
		}
	}
	return nil
}

func (h *ClaudeCodePolicyHandler) put(w http.ResponseWriter, r *http.Request) {
	tenantID, p, ok := h.tenantFromPrincipal(w, r)
	if !ok {
		return
	}

	var req putPolicyRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validatePolicyRules(req.Rules); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := h.store.PutClaudeCodePolicy(r.Context(), tenantID, req.Version, req.Enforce, req.Rules, p.UserID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	// Policy changes are themselves facts in the chain. Best-effort: the DB
	// write already happened; log loudly on failure, never roll back.
	if h.auditor != nil {
		if _, err := h.auditor.Log(r.Context(), tenantID, &audit.AuditEvent{
			Actor:  audit.Actor{ID: p.UserID, Type: audit.ActorHuman},
			Action: "claude_code.policy.updated",
			Resource: audit.Resource{
				ID: "claude_code_policy", Type: "governance_policy",
				Name: "Claude Code Governance Policy",
			},
			Outcome: audit.OutcomeSuccess,
			Metadata: map[string]interface{}{
				"version":    updated.Version,
				"rule_count": len(req.Rules),
				"enforce":    req.Enforce,
			},
		}); err != nil {
			h.logger.Error().Err(err).Str("tenant", tenantID).Msg("failed to audit policy update")
		}
	}

	writeJSON(w, http.StatusOK, updated)
}
