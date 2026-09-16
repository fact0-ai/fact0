// Package auth: dashboard JWT verification.
//
// The dashboard runs Better Auth (https://better-auth.com) inside the
// Next.js process. Its JWT plugin signs Ed25519 tokens and exposes a
// JWKS endpoint at `${BETTER_AUTH_URL}/api/auth/jwks`. We verify those
// tokens here using a refreshing JWKS cache so the Go API never needs
// to talk to the dashboard's session DB.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
)

// Principal is the resolved identity extracted from a verified
// dashboard JWT. The shape is intentionally provider-agnostic - Better
// Auth populates these claims via the JWT plugin's `definePayload`
// (see web/lib/auth.ts):
//
//	sub                      → UserID
//	activeOrganizationId     → OrgID
//	role                     → Role (one of "owner" | "admin" | "member")
//	platformRole             → PlatformRole ("founder" | "support" | "viewer")
type Principal struct {
	UserID       string
	OrgID        string
	Role         string
	PlatformRole string
}

// PrincipalFromContext returns the verified principal, or false if no
// JWT was attached to the request.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	v, ok := ctx.Value(ctxKeyPrincipal).(*Principal)
	return v, ok
}

// Verifier holds a refreshing JWKS cache. Construct one at process
// boot via NewVerifier and reuse the same instance for every request.
type Verifier struct {
	kf       keyfunc.Keyfunc
	issuer   string   // optional; checked when non-empty
	azpAllow []string // optional 'azp' allowlist
	logger   zerolog.Logger
}

// NewVerifier wires up a JWKS cache pointed at jwksURL. The cache
// refreshes in the background; passing a cancellable ctx lets the
// caller stop the goroutine cleanly. issuer and authorizedParties are
// optional - empty means "don't check".
//
// jwksURL is REQUIRED. Production wiring should make startup fail
// loudly when it's missing (the dashboard surface is useless without
// it). Local dev can leave it empty to disable the /v1/me/* routes -
// see RequireJWT, which returns a 404 middleware in that case.
func NewVerifier(ctx context.Context, jwksURL, issuer string, authorizedParties []string, logger zerolog.Logger) (*Verifier, error) {
	if jwksURL == "" {
		return nil, fmt.Errorf("auth: empty jwks url")
	}
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("auth: init jwks: %w", err)
	}
	return &Verifier{
		kf:       kf,
		issuer:   strings.TrimSpace(issuer),
		azpAllow: authorizedParties,
		logger:   logger.With().Str("component", "jwt_verifier").Logger(),
	}, nil
}

// claims models the subset of the Better Auth JWT payload we read.
// jwt.RegisteredClaims handles iss / aud / exp / nbf / iat validation
// automatically inside jwt.ParseWithClaims; we only project the
// custom fields here.
type claims struct {
	jwt.RegisteredClaims
	ActiveOrganizationID string `json:"activeOrganizationId,omitempty"`
	Role                 string `json:"role,omitempty"`
	PlatformRole         string `json:"platformRole,omitempty"`
	AuthorizedParty      string `json:"azp,omitempty"`
}

// Verify parses and validates a bearer token, returning the resolved
// Principal on success. Failures map to descriptive errors suitable
// for logging - callers should NEVER return them to clients verbatim
// (they may leak useful info to attackers); the HTTP middleware
// translates them into a generic 401.
func (v *Verifier) Verify(tokenStr string) (*Principal, error) {
	var c claims
	tok, err := jwt.ParseWithClaims(tokenStr, &c, v.kf.Keyfunc,
		jwt.WithValidMethods([]string{"EdDSA", "RS256", "ES256"}),
	)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if !tok.Valid {
		return nil, fmt.Errorf("token invalid")
	}

	if v.issuer != "" && c.Issuer != v.issuer {
		return nil, fmt.Errorf("issuer mismatch: %q != %q", c.Issuer, v.issuer)
	}
	if len(v.azpAllow) > 0 && c.AuthorizedParty != "" {
		ok := false
		for _, allow := range v.azpAllow {
			if allow == c.AuthorizedParty {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("authorized party %q not allowlisted", c.AuthorizedParty)
		}
	}
	if strings.TrimSpace(c.Subject) == "" {
		return nil, fmt.Errorf("missing sub claim")
	}
	return &Principal{
		UserID:       c.Subject,
		OrgID:        c.ActiveOrganizationID,
		Role:         c.Role,
		PlatformRole: c.PlatformRole,
	}, nil
}

// RequireJWT returns chi middleware that:
//   - 404s every request when no Verifier is configured (so we never
//     leak the surface in dev),
//   - 401s requests without a valid bearer JWT,
//   - injects the Principal into the request context on success.
func RequireJWT(v *Verifier) func(http.Handler) http.Handler {
	if v == nil {
		return func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			})
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				writeAuthError(w, "missing bearer token")
				return
			}
			p, err := v.Verify(raw)
			if err != nil {
				// Log the precise reason server-side so we can debug
				// JWKS / issuer / clock-skew problems without leaking
				// detail to the client.
				v.logger.Warn().
					Err(err).
					Str("path", r.URL.Path).
					Msg("jwt verification failed")
				IncJWTVerifyFailure()
				writeAuthError(w, "invalid or expired token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyPrincipal, p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireOrg is a follow-on middleware that 412s the request if the
// caller is signed in but has no active organization selected. Mount
// it after RequireJWT for any route that needs a tenant.
func RequireOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok || p == nil {
			writeAuthError(w, "not authenticated")
			return
		}
		if strings.TrimSpace(p.OrgID) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPreconditionFailed)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "no active organization - create or select one in the dashboard",
			})
			return
		}
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

// RequireRole gates a route on the org role attached to the active
// session. Better Auth's organization plugin uses "owner" | "admin" |
// "member" by default; pass any combination. Caller must match at
// least one.
//
// Must be mounted AFTER RequireJWT so the principal is populated.
func RequireRole(allowed ...string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		allow[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok || p == nil {
				writeAuthError(w, "not authenticated")
				return
			}
			if _, ok := allow[p.Role]; !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "insufficient organization role",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Role constants matching Better Auth's organization plugin defaults.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// AdminRoles are the roles that may perform tenant-admin actions
// (issue/revoke keys, etc.). Owner is implicitly admin.
var AdminRoles = []string{RoleOwner, RoleAdmin}

// Platform role constants - global founder console access.
const (
	PlatformRoleFounder = "founder"
	PlatformRoleSupport = "support"
	PlatformRoleViewer  = "viewer"
)

// PlatformRolesAll is every role that may reach /v1/admin/* at all.
var PlatformRolesAll = []string{PlatformRoleFounder, PlatformRoleSupport, PlatformRoleViewer}

// PlatformRolesSupportUp may read all admin list/detail endpoints.
var PlatformRolesSupportUp = []string{PlatformRoleFounder, PlatformRoleSupport}

// RequirePlatformRole gates routes on the global platformRole JWT claim.
// Must be mounted AFTER RequireJWT.
func RequirePlatformRole(allowed ...string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		allow[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok || p == nil {
				writeAuthError(w, "not authenticated")
				return
			}
			if _, ok := allow[p.PlatformRole]; !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "insufficient platform role",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxJWKSRefresh caps how long we'll wait for the JWKS endpoint to
// respond when initialising. Exported for tests.
const MaxJWKSRefresh = 5 * time.Second

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="fact0", error="invalid_token"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
