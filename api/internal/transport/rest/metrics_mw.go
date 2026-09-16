package rest

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// MetricsMiddleware times every request and emits fact0_http_*
// metrics. Route labels use the chi route template (e.g.
// `/api/v1/executions/{executionID}/spans`) so dynamic path params
// don't explode Prometheus cardinality.
//
// Mount EARLY in the chain so the timer wraps everything that follows
// - including auth, rate limit, and handler work - and so it sees the
// final status code written by writeError/writeJSON.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &metricsRespWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		route := routeTemplate(r)
		method := r.Method
		dur := time.Since(start).Seconds()

		metrics.HTTPRequestDuration.WithLabelValues(route, method).Observe(dur)
		metrics.HTTPRequestsTotal.
			WithLabelValues(route, method, strconv.Itoa(ww.statusCode)).
			Inc()
	})
}

// routeTemplate returns the matched chi route pattern, or "unknown" if
// no router context is attached. Falling back to the raw URL would
// blow up cardinality for dynamic params, so we deliberately bucket
// unmatched requests together.
func routeTemplate(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "unknown"
	}
	if p := rctx.RoutePattern(); p != "" {
		return p
	}
	// Special-case the synthetic /health endpoint that doesn't go
	// through the chi router pattern matcher in some test setups.
	if r.URL.Path == "/health" {
		return "/health"
	}
	return "unknown"
}

// metricsRespWriter captures the final status code so the middleware
// can label HTTPRequestsTotal correctly.
type metricsRespWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (m *metricsRespWriter) WriteHeader(code int) {
	if !m.wroteHeader {
		m.statusCode = code
		m.wroteHeader = true
	}
	m.ResponseWriter.WriteHeader(code)
}

func (m *metricsRespWriter) Write(b []byte) (int, error) {
	if !m.wroteHeader {
		m.statusCode = http.StatusOK
		m.wroteHeader = true
	}
	return m.ResponseWriter.Write(b)
}

func (m *metricsRespWriter) Flush() {
	if f, ok := m.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var _ http.Flusher = (*metricsRespWriter)(nil)
