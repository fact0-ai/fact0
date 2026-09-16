package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/execution"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// ─── Request Types ──────────────────────────────────────────

type ingestSpansRequest struct {
	Spans []*execution.Span `json:"spans"`
}

type ingestEventsRequest struct {
	Events []*execution.ExecutionEvent `json:"events"`
}

type endExecutionRequest struct {
	Status  execution.Status `json:"status"`
	EndedAt *time.Time       `json:"ended_at,omitempty"`
}

// ─── Response Helpers ───────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeAppError(w http.ResponseWriter, err error) {
	var appErr *apperrors.Error
	if errors.As(err, &appErr) {
		if appErr.Code == apperrors.CodeRateLimited &&
			appErr.RateLimitLimit != nil && appErr.RateLimitUsed != nil {
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"error": "monthly_limit_exceeded",
				"limit": *appErr.RateLimitLimit,
				"used":  *appErr.RateLimitUsed,
			})
			return
		}
		writeJSON(w, appErr.HTTPStatus(), map[string]interface{}{
			"error": map[string]string{
				"code":    string(appErr.Code),
				"message": appErr.Message,
			},
		})
		return
	}
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func intParam(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

// ─── Middleware ──────────────────────────────────────────────

// requestLogger emits one structured log line per HTTP request.
//
// SECURITY: this logger deliberately captures ONLY method, URL path,
// status, and duration. It never logs:
//   - request or response bodies (may contain audit metadata)
//   - r.URL.RawQuery / query-string values (may contain SSE tickets or
//     legacy `?key=` values from clients we don't control)
//   - Authorization headers
//
// If you ever extend this with header or body capture, add an explicit
// allowlist for sensitive paths (anything under /v1/me, /v1/events*,
// /v1/export*, /webhooks/*) and redact bearer credentials.
func requestLogger(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(ww, r)

			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.statusCode).
				Dur("duration", time.Since(start)).
				Msg("request")
		})
	}
}

// corsMiddleware returns an Origin-aware CORS middleware.
//
// When allowedOrigins is empty (local dev) all origins are permitted via "*".
// In production pass the explicit allow-list so that browsers cannot read
// API responses from untrusted pages (prevents CSRF / data-leakage).
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}
	wildcard := len(allowedOrigins) == 0

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if wildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// Vary tells CDNs/proxies not to serve the same response to
				// requests from different origins.
				w.Header().Add("Vary", "Origin")
			}
			// Always set these - they're safe even when the origin is denied.
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the delegate so handlers that require http.Flusher
// (e.g. /v1/events/stream SSE) work through requestLogger wrapping.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var _ http.Flusher = (*responseWriter)(nil)
