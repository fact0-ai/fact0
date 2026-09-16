// Package rest provides the HTTP transport layer.
package rest

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/ingestion"
	"github.com/fact0-ai/fact0/internal/query"
	"github.com/fact0-ai/fact0/internal/replay"
)

// Handler holds all HTTP handlers.
type Handler struct {
	ingestion      *ingestion.Service
	query          *query.Service
	replay         *replay.Service
	logger         zerolog.Logger
	allowedOrigins []string
}

// TelemetryAuth wires API-key ingest and JWT/API-key reads for /api/v1.
type TelemetryAuth struct {
	KeyStore    audit.KeyStore
	Tenants     audit.TenantStore
	JWTVerifier *auth.Verifier
}

// NewHandler creates a new HTTP handler.
func NewHandler(ingestionSvc *ingestion.Service, querySvc *query.Service, replaySvc *replay.Service, logger zerolog.Logger, allowedOrigins []string) *Handler {
	return &Handler{
		ingestion:      ingestionSvc,
		query:          querySvc,
		replay:         replaySvc,
		logger:         logger.With().Str("component", "http").Logger(),
		allowedOrigins: allowedOrigins,
	}
}

// Router creates the chi router with all routes. Used by tests; no auth.
func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()
	h.applyBaseMiddleware(r)
	r.Get("/health", h.health)
	h.MountTelemetry(r, nil, nil)
	return r
}

// MountTelemetry installs /api/v1 telemetry routes on r.
func (h *Handler) MountTelemetry(r chi.Router, rl *RateLimiter, ta *TelemetryAuth) {
	var eventsMW, execMW func(http.Handler) http.Handler
	if rl != nil {
		eventsMW = rl.Events()
		execMW = rl.Executions()
	} else {
		eventsMW = identityMW
		execMW = identityMW
	}

	mountIngest := func(r chi.Router) {
		r.With(execMW).Post("/executions", h.startExecution)
		r.With(eventsMW).Post("/executions/{executionID}/spans", h.ingestSpans)
		r.With(eventsMW).Post("/executions/{executionID}/events", h.ingestEvents)
		r.With(eventsMW).Put("/executions/{executionID}/end", h.endExecution)
	}

	mountRead := func(r chi.Router) {
		r.Get("/executions", h.listExecutions)
		r.Get("/executions/{executionID}", h.getExecution)
		r.Get("/executions/{executionID}/spans", h.getSpans)
		r.Get("/executions/{executionID}/dag", h.getDAG)
		r.Get("/spans/{spanID}", h.getSpan)
		r.Get("/spans/{spanID}/events", h.getSpanEvents)
		r.Get("/executions/{executionID}/replay", h.replayExecution)
	}

	r.Route("/api/v1", func(r chi.Router) {
		if ta != nil {
			r.Group(func(r chi.Router) {
				r.Use(auth.APIKey(ta.KeyStore))
				r.Use(auth.RequireWrite)
				mountIngest(r)
			})
			r.Group(func(r chi.Router) {
				r.Use(auth.DualAuth(ta.KeyStore, ta.Tenants, ta.JWTVerifier))
				mountRead(r)
			})
		} else {
			mountIngest(r)
			mountRead(r)
		}
	})
}

// MountOTLP installs the OTLP/HTTP trace receiver on r.
// The endpoint follows the OTLP/HTTP specification:
//
//	POST /v1/otlp/v1/traces  (Content-Type: application/x-protobuf)
//
// Authentication uses the same API-key + write-scope middleware as the
// native ingest routes. Users configure their OTel SDK/collector with:
//
//	OTEL_EXPORTER_OTLP_ENDPOINT=https://api.fact0.io/v1/otlp
//	OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer alk_live_xxxx"
func MountOTLP(r chi.Router, otlpHandler http.HandlerFunc, ta *TelemetryAuth, rl *RateLimiter) {
	var eventsMW func(http.Handler) http.Handler
	if rl != nil {
		eventsMW = rl.Events()
	} else {
		eventsMW = identityMW
	}

	r.Route("/v1/otlp", func(r chi.Router) {
		if ta != nil {
			r.Use(auth.APIKey(ta.KeyStore))
			r.Use(auth.RequireWrite)
		}
		r.With(eventsMW).Post("/v1/traces", otlpHandler)
	})
}

func tenantFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := auth.TenantFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	return tenantID, true
}

// identityMW is a no-op middleware used when rate limiting is disabled.
func identityMW(next http.Handler) http.Handler { return next }

// ApplyBaseMiddleware installs the standard chi middleware stack on r.
func (h *Handler) ApplyBaseMiddleware(r chi.Router) { h.applyBaseMiddleware(r) }

func (h *Handler) applyBaseMiddleware(r chi.Router) {
	r.Use(RequestBodyLimit)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(h.logger))
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(h.allowedOrigins))
	r.Use(middleware.Compress(5))
}

// ─── Health ─────────────────────────────────────────────────

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "fact0"})
}

// ─── Ingestion Handlers ─────────────────────────────────────

func (h *Handler) startExecution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}

	var req ingestion.StartExecutionRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	exec, err := h.ingestion.StartExecution(r.Context(), tenantID, req)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, exec)
}

func (h *Handler) ingestSpans(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	var req ingestSpansRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	accepted, errors := h.ingestion.IngestSpans(r.Context(), tenantID, executionID, req.Spans)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accepted_count": accepted,
		"errors":         errors,
	})
}

func (h *Handler) ingestEvents(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	var req ingestEventsRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	accepted, errors := h.ingestion.IngestEvents(r.Context(), tenantID, executionID, req.Events)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accepted_count": accepted,
		"errors":         errors,
	})
}

func (h *Handler) endExecution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	var req endExecutionRequest
	if err := requestJSONDecoder(r).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	exec, err := h.ingestion.EndExecutionAt(r.Context(), tenantID, executionID, req.Status, req.EndedAt)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, exec)
}

// ─── Query Handlers ─────────────────────────────────────────

func (h *Handler) listExecutions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}

	req := query.ListExecutionsRequest{
		AgentID:  r.URL.Query().Get("agent_id"),
		Status:   r.URL.Query().Get("status"),
		PageSize: intParam(r, "page_size", 20),
		Offset:   intParam(r, "offset", 0),
	}

	resp, err := h.query.ListExecutions(r.Context(), tenantID, req)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getExecution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	exec, err := h.query.GetExecution(r.Context(), tenantID, executionID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, exec)
}

func (h *Handler) getSpans(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	spans, err := h.query.GetSpansByExecution(r.Context(), tenantID, executionID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"spans": spans})
}

func (h *Handler) getDAG(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	nodes, edges, err := h.query.GetExecutionDAG(r.Context(), tenantID, executionID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes, "edges": edges})
}

func (h *Handler) getSpan(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	spanID := chi.URLParam(r, "spanID")

	span, events, err := h.query.GetSpan(r.Context(), tenantID, spanID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"span": span, "events": events})
}

func (h *Handler) getSpanEvents(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	spanID := chi.URLParam(r, "spanID")

	events, err := h.query.GetSpanEvents(r.Context(), tenantID, spanID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"events": events})
}

// ─── Replay Handlers ────────────────────────────────────────

func (h *Handler) replayExecution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	executionID := chi.URLParam(r, "executionID")

	req := replay.ReplayRequest{
		ExecutionID:  executionID,
		FromSequence: int64(intParam(r, "from_sequence", 0)),
		ToSequence:   int64(intParam(r, "to_sequence", 0)),
	}

	resp, err := h.replay.ReplayExecution(r.Context(), tenantID, req)
	if err != nil {
		writeAppError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
