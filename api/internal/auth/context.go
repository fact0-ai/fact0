package auth

import (
	"context"

	"github.com/fact0-ai/fact0/internal/audit"
)

type ctxKey int

const (
	ctxKeyTenant ctxKey = iota
	ctxKeyScope
	ctxKeyKeyID
	ctxKeyPrincipal
)

// WithTenant attaches the resolved tenant id, scope and key id to ctx.
func WithTenant(ctx context.Context, tenantID string, scope audit.Scope, keyID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyTenant, tenantID)
	ctx = context.WithValue(ctx, ctxKeyScope, scope)
	ctx = context.WithValue(ctx, ctxKeyKeyID, keyID)
	return ctx
}

// TenantFromContext returns the resolved tenant id, or "" if none.
func TenantFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTenant).(string)
	return v
}

// ScopeFromContext returns the resolved scope, or "" if none.
func ScopeFromContext(ctx context.Context) audit.Scope {
	v, _ := ctx.Value(ctxKeyScope).(audit.Scope)
	return v
}

// KeyIDFromContext returns the api_keys.id used to authenticate.
func KeyIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyKeyID).(string)
	return v
}

// WithPrincipal attaches a verified JWT principal to ctx. Exists so tests can
// exercise JWT-gated handlers without a full verifier; production code paths
// only set it via RequireJWT.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal, p)
}
