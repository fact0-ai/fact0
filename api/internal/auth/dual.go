package auth

import (
	"encoding/json"
	"net/http"

	"github.com/fact0-ai/fact0/internal/audit"
)

// DualAuth returns middleware for read-only audit routes that accepts
// EITHER:
//
//   - an `alk_live_*` bearer API key (used by SDK clients like the
//     Python SDK), OR
//   - a verified dashboard JWT belonging to a member of an org mapped
//     to a backend tenant (used by the Next.js dashboard).
//
// On JWT success the resolved (tenantID, ScopeRead) is injected into
// the request context via WithTenant - exactly the same shape API key
// auth produces, so every downstream audit handler stays unchanged.
// The synthetic key id is "user:<userID>" for log/audit attribution.
//
// Write scope is NEVER granted via the dashboard JWT path. Use
// RequireWrite on top of this middleware to gate ingestion endpoints,
// but the cleaner pattern is to wire write routes with the raw APIKey
// middleware (no dual auth) so the dashboard physically cannot reach
// a write endpoint. This is what makes the browser XSS-safe: even if
// a malicious script captures the user's JWT, it can only read the
// audit log, never forge events.
func DualAuth(keys audit.KeyStore, tenants audit.TenantStore, v *Verifier) func(http.Handler) http.Handler {
	apiKeyMW := APIKey(keys)
	jwtMW := RequireJWT(v)

	return func(next http.Handler) http.Handler {
		jwtBridge := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok || p == nil || p.OrgID == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPreconditionFailed)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "no active organization",
				})
				return
			}
			tenant, err := tenants.GetTenantByExternalID(r.Context(), audit.ProviderBetterAuthOrg, p.OrgID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "tenant not provisioned - POST /v1/me/bootstrap first",
				})
				return
			}
			ctx := WithTenant(r.Context(), tenant.ID, audit.ScopeRead, "user:"+p.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If the caller presents an Authorization header that looks
			// like our API key shape, take the SDK path. Otherwise fall
			// through to JWT verification.
			if LooksLikeKey(bearerToken(r)) {
				apiKeyMW(next).ServeHTTP(w, r)
				return
			}
			jwtMW(jwtBridge).ServeHTTP(w, r)
		})
	}
}

// SSEAuth gates the SSE streaming endpoint. It accepts:
//
//   - `?ticket=tkt_*` - short-lived single-use token the dashboard
//     exchanges its JWT for via POST /v1/me/sse-ticket.
//   - A bearer API key (SDK path, same as APIKey middleware).
//
// We do not accept `?key=<API key>` here - putting long-lived
// credentials in URLs leaks them to access logs, browser history and
// Referer chains. The ticket dance is half the security cost.
func SSEAuth(tickets *SSETicketStore, keys audit.KeyStore) func(http.Handler) http.Handler {
	apiKeyMW := APIKey(keys)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok := r.URL.Query().Get("ticket"); tok != "" {
				t, ok := tickets.Consume(tok)
				if !ok {
					IncSSETicketFailure()
					writeUnauthorized(w, "invalid or expired sse ticket")
					return
				}
				ctx := WithTenant(r.Context(), t.TenantID, t.Scope, "ticket")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			apiKeyMW(next).ServeHTTP(w, r)
		})
	}
}
