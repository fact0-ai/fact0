package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fact0-ai/fact0/internal/audit"
)

// APIKey returns chi middleware that resolves the bearer API key on the
// request, looks it up via store, and injects (tenant, scope, key id) into
// the request context. Missing, malformed, unknown or revoked keys → 401.
func APIKey(store audit.KeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if !LooksLikeKey(raw) {
				writeUnauthorized(w, "missing or malformed api key")
				return
			}
			id := ""
			stripped := strings.TrimPrefix(raw, KeyPrefixLive)
			parts := strings.SplitN(stripped, ".", 2)
			if len(parts) == 2 {
				id = parts[0]
			} else {
				writeUnauthorized(w, "invalid api key format")
				return
			}

			k, err := store.GetKeyByID(r.Context(), id)
			if err != nil {
				writeUnauthorized(w, "invalid api key")
				return
			}

			_, ok := VerifyAPIKey(raw, k.Hash)
			if !ok {
				writeUnauthorized(w, "invalid api key")
				return
			}
			if k.RevokedAt != nil {
				writeUnauthorized(w, "api key revoked")
				return
			}
			ctx := WithTenant(r.Context(), k.TenantID, k.Scope, k.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireWrite returns middleware that 403s any request whose resolved
// scope is not write. Mount it on ingestion routes only.
func RequireWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ScopeFromContext(r.Context()).CanWrite() {
			writeForbidden(w, "write scope required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireWriteOrAdmin gates routes to write-scoped API keys OR dashboard owners/admins.
func RequireWriteOrAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if p, ok := PrincipalFromContext(ctx); ok && p != nil {
			if p.Role != RoleOwner && p.Role != RoleAdmin {
				writeForbidden(w, "admin role required to perform this action")
				return
			}
		} else {
			if !ScopeFromContext(ctx).CanWrite() {
				writeForbidden(w, "write scope required to perform this action")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// bearerToken extracts the API key from the Authorization header.
//
// We deliberately do NOT support `?key=<token>` query-string fallbacks.
// Putting credentials in URLs leaks them to access logs, browser
// history, Referer chains and any extension that reads URLs. Clients
// that cannot set headers (EventSource) should call POST /v1/me/sse-ticket
// and connect with `?ticket=` instead, which is single-use and 60s-TTL.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, prefix))
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="fact0"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
