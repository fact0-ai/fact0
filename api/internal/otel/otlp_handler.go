package otel

import (
	"io"
	"net/http"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/ingestion"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// Handler serves the OTLP/HTTP trace receiver endpoint.
// It accepts ExportTraceServiceRequest (protobuf or JSON) and translates
// them into Fact0 executions + spans via the Translator.
type Handler struct {
	ingestion  *ingestion.Service
	translator *Translator
	logger     zerolog.Logger
}

// NewHandler creates an OTLP/HTTP handler.
func NewHandler(ingestionSvc *ingestion.Service, logger zerolog.Logger) *Handler {
	return &Handler{
		ingestion:  ingestionSvc,
		translator: NewTranslator(),
		logger:     logger.With().Str("component", "otlp-http").Logger(),
	}
}

// ExportTraces handles POST /v1/otlp/v1/traces (OTLP/HTTP spec).
//
// Accepts:
//   - Content-Type: application/x-protobuf (binary protobuf, preferred)
//   - Content-Type: application/json (JSON-encoded protobuf, convenience)
//
// Authentication: API key via Authorization header (same as /api/v1 ingest).
// The tenant ID is extracted from the authenticated context.
func (h *Handler) ExportTraces(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	if tenantID == "" {
		h.writeOTLPError(w, http.StatusUnauthorized, "unauthorized: missing or invalid API key")
		return
	}

	// Read body (capped at 4MB to match existing MaxBatchBodyBytes default).
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		h.writeOTLPError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Parse OTLP ExportTraceServiceRequest.
	var req collectorpb.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		h.writeOTLPError(w, http.StatusBadRequest, "failed to parse OTLP protobuf: "+err.Error())
		return
	}

	// Translate OTel → Fact0.
	traces := h.translator.TranslateResourceSpans(tenantID, req.ResourceSpans)
	if len(traces) == 0 {
		h.writeOTLPSuccess(w)
		return
	}

	// Ingest into Fact0.
	totalSpans := 0
	totalExecs := 0
	for _, tt := range traces {
		// Upsert the execution (start or update).
		_, err := h.ingestion.StartExecution(r.Context(), tenantID, ingestion.StartExecutionRequest{
			AgentID:        tt.Execution.AgentID,
			AgentName:      tt.Execution.AgentName,
			Trigger:        tt.Execution.Trigger,
			Metadata:       tt.Execution.Metadata,
			IdempotencyKey: tt.Execution.ID,
		})
		if err != nil {
			h.logger.Warn().Err(err).
				Str("execution_id", tt.Execution.ID).
				Str("tenant_id", tenantID).
				Msg("failed to start execution from OTLP trace")
			metrics.IngestRejected.WithLabelValues("otlp_execution", "storage").Inc()
			continue
		}
		totalExecs++

		// Ingest all spans.
		if len(tt.Spans) > 0 {
			accepted, errs := h.ingestion.IngestSpans(r.Context(), tenantID, tt.Execution.ID, tt.Spans)
			totalSpans += accepted
			if len(errs) > 0 {
				h.logger.Warn().
					Str("execution_id", tt.Execution.ID).
					Int("accepted", accepted).
					Int("errors", len(errs)).
					Msg("partial OTLP span ingestion")
			}
		}

		// If the execution is complete, end it.
		if tt.Execution.Status == "COMPLETED" || tt.Execution.Status == "FAILED" {
			_, _ = h.ingestion.EndExecution(r.Context(), tenantID, tt.Execution.ID, tt.Execution.Status)
		}
	}

	metrics.IngestAccepted.WithLabelValues("otlp_trace").Add(float64(totalExecs))
	metrics.IngestAccepted.WithLabelValues("otlp_span").Add(float64(totalSpans))

	h.logger.Info().
		Str("tenant_id", tenantID).
		Int("traces", len(traces)).
		Int("executions", totalExecs).
		Int("spans", totalSpans).
		Msg("OTLP traces ingested")

	h.writeOTLPSuccess(w)
}

// writeOTLPSuccess writes a 200 OK with an empty ExportTraceServiceResponse.
func (h *Handler) writeOTLPSuccess(w http.ResponseWriter) {
	resp := &collectorpb.ExportTraceServiceResponse{}
	data, _ := proto.Marshal(resp)
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// writeOTLPError writes an error response.
func (h *Handler) writeOTLPError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
