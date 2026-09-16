// Package otel translates OpenTelemetry (OTLP) trace data into Fact0's
// internal domain model. This is the bridge that makes Fact0 "OTel native":
// any application exporting OTLP traces can send them to Fact0 with zero
// code changes - just set OTEL_EXPORTER_OTLP_ENDPOINT.
package otel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/fact0-ai/fact0/internal/execution"
)

// ─── Constants ──────────────────────────────────────────────

// Well-known OTel semantic convention attribute keys.
const (
	// Resource attributes
	attrServiceName       = "service.name"
	attrServiceVersion    = "service.version"
	attrServiceInstanceID = "service.instance.id"

	// GenAI semantic conventions (https://opentelemetry.io/docs/specs/semconv/gen-ai/)
	attrGenAISystem           = "gen_ai.system"
	attrGenAIRequestModel     = "gen_ai.request.model"
	attrGenAIPromptTokens     = "gen_ai.usage.prompt_tokens"
	attrGenAICompletionTokens = "gen_ai.usage.completion_tokens"
	attrGenAITotalTokens      = "gen_ai.usage.total_tokens"
	attrGenAITemperature      = "gen_ai.request.temperature"

	// Tool semantic conventions
	attrToolName = "tool.name"

	// Fact0-specific override attribute - users can set this explicitly
	// to force a specific SpanKind regardless of heuristics.
	attrFact0SpanType = "fact0.span_type"
)

// ─── Translator ─────────────────────────────────────────────

// Translator converts OTLP trace data to Fact0 domain objects.
type Translator struct {
	enricher *Enricher
}

// NewTranslator creates a new OTel→Fact0 translator with semantic
// convention enrichment enabled.
func NewTranslator() *Translator {
	return &Translator{
		enricher: NewEnricher(),
	}
}

// TranslatedTrace holds the Fact0 domain objects produced from a single OTLP trace.
type TranslatedTrace struct {
	// Execution is auto-created from the root span's resource attributes.
	Execution *execution.Execution

	// Spans are the translated Fact0 spans, in the same order as the OTLP input.
	Spans []*execution.Span
}

// spanEntry pairs an OTLP span with its parent resource.
type spanEntry struct {
	resource *resourcepb.Resource
	span     *tracepb.Span
}

// TranslateResourceSpans converts a batch of OTLP ResourceSpans into Fact0
// domain objects, grouped by traceID (one TranslatedTrace per unique trace).
func (t *Translator) TranslateResourceSpans(tenantID string, resourceSpans []*tracepb.ResourceSpans) []*TranslatedTrace {
	// Group all spans by traceID so we can create one Execution per trace.
	byTrace := make(map[string][]spanEntry)
	traceOrder := make([]string, 0) // preserve order

	for _, rs := range resourceSpans {
		for _, scopeSpans := range rs.ScopeSpans {
			for _, span := range scopeSpans.Spans {
				traceID := hex.EncodeToString(span.TraceId)
				if _, exists := byTrace[traceID]; !exists {
					traceOrder = append(traceOrder, traceID)
				}
				byTrace[traceID] = append(byTrace[traceID], spanEntry{
					resource: rs.Resource,
					span:     span,
				})
			}
		}
	}

	results := make([]*TranslatedTrace, 0, len(byTrace))
	for _, traceID := range traceOrder {
		entries := byTrace[traceID]
		tt := t.translateTrace(tenantID, traceID, entries)
		results = append(results, tt)
	}
	return results
}

func (t *Translator) translateTrace(tenantID, traceID string, entries []spanEntry) *TranslatedTrace {
	// Use the first entry's resource to derive the Execution.
	firstResource := entries[0].resource
	resourceAttrs := flattenAttributes(firstResource.GetAttributes())

	executionID := deriveExecutionID(tenantID, traceID)
	agentID := resourceAttrs[attrServiceName]
	if agentID == "" {
		agentID = "unknown-service"
	}
	agentName := agentID
	if instanceID, ok := resourceAttrs[attrServiceInstanceID]; ok {
		agentName = fmt.Sprintf("%s/%s", agentID, instanceID)
	}

	// Find the earliest start time to use as execution start.
	var earliestStart time.Time
	for _, e := range entries {
		start := nanoToTime(e.span.StartTimeUnixNano)
		if earliestStart.IsZero() || start.Before(earliestStart) {
			earliestStart = start
		}
	}

	// Execution metadata includes the OTel traceID for cross-reference
	// and all resource attributes.
	execMeta := map[string]string{
		"otel.trace_id": traceID,
		"otel.source":   "otlp",
	}
	for k, v := range resourceAttrs {
		execMeta["resource."+k] = v
	}

	exec := &execution.Execution{
		ID:        executionID,
		AgentID:   agentID,
		AgentName: agentName,
		Status:    execution.StatusRunning,
		Trigger:   "otlp",
		StartedAt: earliestStart,
		Metadata:  execMeta,
	}

	// Determine if all spans have ended → mark execution completed/failed.
	allEnded := true
	anyFailed := false
	var latestEnd time.Time
	for _, e := range entries {
		if e.span.EndTimeUnixNano == 0 {
			allEnded = false
		} else {
			end := nanoToTime(e.span.EndTimeUnixNano)
			if latestEnd.IsZero() || end.After(latestEnd) {
				latestEnd = end
			}
		}
		if e.span.Status != nil && e.span.Status.Code == tracepb.Status_STATUS_CODE_ERROR {
			anyFailed = true
		}
	}
	if allEnded {
		exec.EndedAt = &latestEnd
		if anyFailed {
			exec.Status = execution.StatusFailed
		} else {
			exec.Status = execution.StatusCompleted
		}
	}

	// Translate individual spans.
	spans := make([]*execution.Span, 0, len(entries))
	for i, e := range entries {
		span := t.translateSpan(executionID, e.span, i)
		spans = append(spans, span)
	}

	return &TranslatedTrace{
		Execution: exec,
		Spans:     spans,
	}
}

// translateSpan converts a single OTLP span to a Fact0 span.
func (t *Translator) translateSpan(executionID string, otelSpan *tracepb.Span, index int) *execution.Span {
	spanID := hex.EncodeToString(otelSpan.SpanId)
	attrs := flattenAttributes(otelSpan.Attributes)

	// Derive parent span ID.
	var parentSpanID *string
	if len(otelSpan.ParentSpanId) > 0 {
		pid := hex.EncodeToString(otelSpan.ParentSpanId)
		// A zero parent means root span - don't set parent.
		if pid != "0000000000000000" {
			parentSpanID = &pid
		}
	}

	startedAt := nanoToTime(otelSpan.StartTimeUnixNano)
	var endedAt *time.Time
	if otelSpan.EndTimeUnixNano > 0 {
		t := nanoToTime(otelSpan.EndTimeUnixNano)
		endedAt = &t
	}

	// Infer Fact0 SpanKind from attributes + OTel span kind.
	spanType := inferSpanType(attrs, otelSpan.Kind)

	// Map OTel status to Fact0 status.
	status := mapStatus(otelSpan.Status, endedAt)

	// Build metadata - include OTel span ID for cross-reference and all
	// span attributes that aren't consumed by type-specific details.
	meta := map[string]string{
		"otel.span_id": spanID,
	}
	for k, v := range attrs {
		meta[k] = v
	}

	span := &execution.Span{
		ID:           spanID,
		ExecutionID:  executionID,
		ParentSpanID: parentSpanID,
		SpanType:     spanType,
		Name:         otelSpan.Name,
		Status:       status,
		StartedAt:    startedAt,
		EndedAt:      endedAt,
		Metadata:     meta,
	}

	// Enrich with type-specific details.
	switch spanType {
	case execution.SpanKindModelInvocation:
		span.ModelInvocation = extractModelInvocation(attrs, span.DurationMs())
	case execution.SpanKindToolCall:
		span.ToolCall = extractToolCall(attrs, span.DurationMs())
	}

	// Extract error info.
	if otelSpan.Status != nil && otelSpan.Status.Code == tracepb.Status_STATUS_CODE_ERROR {
		span.Error = &execution.SpanError{
			Code:    "OTEL_ERROR",
			Message: otelSpan.Status.Message,
		}
	}

	// Post-process: apply semantic convention enrichment.
	// This extracts GenAI prompt/completion from span events, upgrades
	// db.* and http.* spans to TOOL_CALL, and enriches exception events.
	if t.enricher != nil {
		t.enricher.Enrich(span, otelSpan)
	}

	return span
}

// ─── SpanKind Inference ─────────────────────────────────────

// inferSpanType uses a priority-based heuristic to determine the Fact0 SpanKind:
//  1. Explicit fact0.span_type attribute (user override)
//  2. GenAI semantic conventions (gen_ai.system present → MODEL_INVOCATION)
//  3. Tool conventions (tool.name present → TOOL_CALL)
//  4. Default to CUSTOM
func inferSpanType(attrs map[string]string, kind tracepb.Span_SpanKind) execution.SpanKind {
	// Priority 1: Explicit override.
	if explicit, ok := attrs[attrFact0SpanType]; ok {
		switch strings.ToUpper(explicit) {
		case "TOOL_CALL":
			return execution.SpanKindToolCall
		case "MODEL_INVOCATION":
			return execution.SpanKindModelInvocation
		case "STATE_MUTATION":
			return execution.SpanKindStateMutation
		case "HUMAN_APPROVAL":
			return execution.SpanKindHumanApproval
		case "POLICY_EVALUATION":
			return execution.SpanKindPolicyEvaluation
		}
	}

	// Priority 2: GenAI semantic conventions.
	if _, ok := attrs[attrGenAISystem]; ok {
		return execution.SpanKindModelInvocation
	}
	if _, ok := attrs[attrGenAIRequestModel]; ok {
		return execution.SpanKindModelInvocation
	}

	// Priority 3: Tool conventions.
	if _, ok := attrs[attrToolName]; ok {
		return execution.SpanKindToolCall
	}

	return execution.SpanKindCustom
}

// ─── Type-specific Detail Extractors ────────────────────────

func extractModelInvocation(attrs map[string]string, durationMs int64) *execution.ModelInvocationDetail {
	detail := &execution.ModelInvocationDetail{
		ModelName:     attrs[attrGenAIRequestModel],
		ModelProvider: attrs[attrGenAISystem],
		LatencyMs:     durationMs,
	}
	if v, err := strconv.ParseInt(attrs[attrGenAIPromptTokens], 10, 32); err == nil {
		detail.PromptTokens = int32(v)
	}
	if v, err := strconv.ParseInt(attrs[attrGenAICompletionTokens], 10, 32); err == nil {
		detail.CompletionTokens = int32(v)
	}
	if v, err := strconv.ParseInt(attrs[attrGenAITotalTokens], 10, 32); err == nil {
		detail.TotalTokens = int32(v)
	} else {
		detail.TotalTokens = detail.PromptTokens + detail.CompletionTokens
	}
	if v, err := strconv.ParseFloat(attrs[attrGenAITemperature], 64); err == nil {
		detail.Temperature = v
	}

	// Session extraction
	if sessionID, ok := attrs["session_id"]; ok {
		detail.SessionID = sessionID
	} else if sessionID, ok := attrs["session.id"]; ok {
		detail.SessionID = sessionID
	}

	// Turn sequence extraction
	if turnSeqStr, ok := attrs["turn_sequence"]; ok {
		if v, err := strconv.Atoi(turnSeqStr); err == nil {
			detail.TurnSequence = v
		}
	} else if turnSeqStr, ok := attrs["turn.sequence"]; ok {
		if v, err := strconv.Atoi(turnSeqStr); err == nil {
			detail.TurnSequence = v
		}
	}

	// Prompt name extraction
	if promptName, ok := attrs["prompt_name"]; ok {
		detail.PromptName = promptName
	} else if promptName, ok := attrs["prompt.name"]; ok {
		detail.PromptName = promptName
	}

	// Prompt version extraction
	if promptVerStr, ok := attrs["prompt_version"]; ok {
		if v, err := strconv.Atoi(promptVerStr); err == nil {
			detail.PromptVersion = v
		}
	} else if promptVerStr, ok := attrs["prompt.version"]; ok {
		if v, err := strconv.Atoi(promptVerStr); err == nil {
			detail.PromptVersion = v
		}
	}

	// Cost extraction
	if costStr, ok := attrs["cost_usd"]; ok {
		if v, err := strconv.ParseFloat(costStr, 64); err == nil {
			detail.CostUSD = v
		}
	} else if costStr, ok := attrs["cost.usd"]; ok {
		if v, err := strconv.ParseFloat(costStr, 64); err == nil {
			detail.CostUSD = v
		}
	} else if costStr, ok := attrs["gen_ai.usage.cost"]; ok {
		if v, err := strconv.ParseFloat(costStr, 64); err == nil {
			detail.CostUSD = v
		}
	}

	return detail
}

func extractToolCall(attrs map[string]string, durationMs int64) *execution.ToolCallDetail {
	return &execution.ToolCallDetail{
		ToolName:    attrs[attrToolName],
		ToolVersion: attrs["tool.version"],
		DurationMs:  durationMs,
	}
}

// ─── Helpers ────────────────────────────────────────────────

// deriveExecutionID creates a deterministic Fact0 execution ID from the
// tenant ID and OTel trace ID. This ensures idempotency: sending the same
// trace twice produces the same execution ID.
func deriveExecutionID(tenantID, traceID string) string {
	sum := sha256.Sum256([]byte(tenantID + ":" + traceID))
	return "otel_" + hex.EncodeToString(sum[:10]) // 20 hex chars
}

// nanoToTime converts nanoseconds since Unix epoch to time.Time.
func nanoToTime(nanos uint64) time.Time {
	return time.Unix(0, int64(nanos)).UTC()
}

// mapStatus converts OTel status + endedness to a Fact0 Status.
func mapStatus(s *tracepb.Status, endedAt *time.Time) execution.Status {
	if s != nil && s.Code == tracepb.Status_STATUS_CODE_ERROR {
		return execution.StatusFailed
	}
	if endedAt != nil {
		return execution.StatusCompleted
	}
	return execution.StatusRunning
}

// flattenAttributes converts OTel KeyValue attributes to a simple string map.
// Complex types (arrays, nested kvlists) are JSON-ish stringified.
func flattenAttributes(attrs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		m[kv.Key] = anyValueToString(kv.Value)
	}
	return m
}

// anyValueToString extracts a string from an OTel AnyValue.
func anyValueToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(val.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(val.DoubleValue, 'f', -1, 64)
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(val.BoolValue)
	case *commonpb.AnyValue_BytesValue:
		return hex.EncodeToString(val.BytesValue)
	default:
		return fmt.Sprintf("%v", v.Value)
	}
}
