package otel

import (
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/fact0-ai/fact0/internal/execution"
)

// Enricher post-processes translated Fact0 spans to extract structured
// details from OTel semantic conventions. It runs after the basic
// Translator mapping and before ingestion, upgrading generic CUSTOM spans
// into domain-specific types (MODEL_INVOCATION, TOOL_CALL) when the
// right semantic convention attributes are present.
type Enricher struct{}

// NewEnricher creates a new semantic convention enricher.
func NewEnricher() *Enricher { return &Enricher{} }

// Enrich applies all enrichment rules to a translated span.
// It inspects both span attributes (already in span.Metadata) and
// OTel span events (prompt/completion content, exceptions).
func (e *Enricher) Enrich(span *execution.Span, otelSpan *tracepb.Span) {
	attrs := span.Metadata

	// 1. GenAI span event enrichment (prompt/completion payloads).
	e.enrichGenAIEvents(span, otelSpan)

	// 2. Database → ToolCall enrichment.
	// When db.system is present and the span hasn't been classified yet,
	// treat it as a tool call against a database.
	if dbSystem, ok := attrs[SemConvDBSystem]; ok && span.SpanType == execution.SpanKindCustom {
		span.SpanType = execution.SpanKindToolCall
		span.ToolCall = &execution.ToolCallDetail{
			ToolName:    "db:" + dbSystem,
			ToolVersion: attrs[SemConvDBName],
			DurationMs:  span.DurationMs(),
		}
		if stmt, ok := attrs[SemConvDBStatement]; ok {
			span.ToolCall.Input = &execution.PayloadRef{
				Inline:      stmt,
				SizeBytes:   int64(len(stmt)),
				ContentType: "text/sql",
			}
		}
	}

	// 3. HTTP → ToolCall enrichment.
	// When http.request.method is present and the span is unclassified,
	// treat it as an HTTP API tool call.
	if method, ok := attrs[SemConvHTTPMethod]; ok && span.SpanType == execution.SpanKindCustom {
		span.SpanType = execution.SpanKindToolCall
		toolName := "http:" + method
		if url, ok := attrs[SemConvHTTPURL]; ok {
			toolName = method + " " + url
		}
		span.ToolCall = &execution.ToolCallDetail{
			ToolName:   toolName,
			DurationMs: span.DurationMs(),
		}
		if statusCode, ok := attrs[SemConvHTTPStatusCode]; ok {
			span.ToolCall.Output = &execution.PayloadRef{
				Inline:    map[string]string{"status_code": statusCode},
				SizeBytes: int64(len(statusCode)),
			}
		}
	}

	// 4. Exception event → SpanError enrichment.
	e.enrichExceptionEvents(span, otelSpan)
}

// enrichGenAIEvents extracts prompt/completion content from OTel span events.
// The GenAI SIG specification stores prompt and completion as span events
// (not attributes), so we must inspect the events array.
func (e *Enricher) enrichGenAIEvents(span *execution.Span, otelSpan *tracepb.Span) {
	if span.ModelInvocation == nil {
		return
	}
	for _, event := range otelSpan.Events {
		eventAttrs := flattenAttributes(event.Attributes)
		switch event.Name {
		case "gen_ai.content.prompt":
			if content, ok := eventAttrs["gen_ai.prompt"]; ok {
				span.ModelInvocation.Prompt = &execution.PayloadRef{
					Inline:      content,
					SizeBytes:   int64(len(content)),
					ContentType: "text/plain",
				}
			}
		case "gen_ai.content.completion":
			if content, ok := eventAttrs["gen_ai.completion"]; ok {
				span.ModelInvocation.Completion = &execution.PayloadRef{
					Inline:      content,
					SizeBytes:   int64(len(content)),
					ContentType: "text/plain",
				}
			}
		}
	}
}

// enrichExceptionEvents extracts exception details from OTel span events.
// OTel records exceptions as events named "exception" with type, message,
// and stacktrace attributes.
func (e *Enricher) enrichExceptionEvents(span *execution.Span, otelSpan *tracepb.Span) {
	if span.Error != nil {
		return // Already has error from OTel status
	}
	for _, event := range otelSpan.Events {
		if event.Name == "exception" {
			eventAttrs := flattenAttributes(event.Attributes)
			span.Error = &execution.SpanError{
				Code:       eventAttrs[SemConvExceptionType],
				Message:    eventAttrs[SemConvExceptionMessage],
				StackTrace: eventAttrs[SemConvExceptionStacktrace],
			}
			if span.Status != execution.StatusFailed {
				span.Status = execution.StatusFailed
			}
			break // Only take the first exception
		}
	}
}
