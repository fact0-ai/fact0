package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/share"
)

type ctxKeyShare struct{}

// ShareContext holds resolved share-link authorization.
type ShareContext struct {
	LinkID   string
	TenantID string
	From     *time.Time
	To       *time.Time
}

// WithShare attaches share-link context.
func WithShare(ctx context.Context, sc ShareContext) context.Context {
	ctx = WithTenant(ctx, sc.TenantID, audit.ScopeRead, "share:"+sc.LinkID)
	return context.WithValue(ctx, ctxKeyShare{}, sc)
}

// ShareFromContext returns share context when present.
func ShareFromContext(ctx context.Context) (ShareContext, bool) {
	v, ok := ctx.Value(ctxKeyShare{}).(ShareContext)
	return v, ok
}

// ShareLinkResolver loads share links for middleware.
type ShareLinkResolver interface {
	GetByIDWithSecret(ctx context.Context, id string) (*share.Link, string, error)
	RecordAccess(ctx context.Context, id string) error
}

// ShareAuth validates X-Fact0-Share-Token or Bearer shr_* token.
func ShareAuth(resolver ShareLinkResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := r.Header.Get("X-Fact0-Share-Token")
			if tok == "" {
				if h := bearerToken(r); h != "" {
					tok = h
				}
			}
			if tok == "" {
				tok = r.URL.Query().Get("share")
			}
			if tok == "" {
				writeUnauthorized(w, "missing share token")
				return
			}

			parts := strings.SplitN(tok, "_", 3) // shr_ULID_SECRET
			if len(parts) != 3 {
				writeUnauthorized(w, "invalid share token format")
				return
			}
			id := parts[0] + "_" + parts[1]
			secretStr := parts[2]

			link, secretHash, err := resolver.GetByIDWithSecret(r.Context(), id)
			if err != nil {
				writeUnauthorized(w, "invalid share link")
				return
			}

			expectedHash := Hash(secretStr)
			if subtle.ConstantTimeCompare([]byte(expectedHash), []byte(secretHash)) != 1 {
				writeUnauthorized(w, "invalid share secret")
				return
			}
			if !link.Valid(time.Now().UTC()) {
				writeErrorJSON(w, http.StatusGone, "share link expired or revoked")
				return
			}
			_ = resolver.RecordAccess(r.Context(), link.ID)

			ctx := WithShare(r.Context(), ShareContext{
				LinkID:   link.ID,
				TenantID: link.TenantID,
				From:     link.FilterFrom,
				To:       link.FilterTo,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

// MergeShareRange intersects query range with share link bounds.
func MergeShareRange(ctx context.Context, from, to time.Time) (time.Time, time.Time) {
	sc, ok := ShareFromContext(ctx)
	if !ok {
		return from, to
	}
	if sc.From != nil && (from.IsZero() || sc.From.After(from)) {
		from = *sc.From
	}
	if sc.To != nil && (to.IsZero() || sc.To.Before(to)) {
		to = *sc.To
	}
	return from, to
}
