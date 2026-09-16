package otel

import (
	"encoding/hex"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/fact0-ai/fact0/internal/execution"
)

func TestTranslateResourceSpans_BasicTrace(t *testing.T) {
	translator := NewTranslator()

	traceID, _ := hex.DecodeString("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := hex.DecodeString("b7ad6b7169203331")
	now := time.Now().UTC()
	startNano := uint64(now.UnixNano())
	endNano := uint64(now.Add(100 * time.Millisecond).UnixNano())

	rs := []*tracepb.ResourceSpans{
		{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					{Key: attrServiceName, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "my-agent"}}},
				},
			},
			ScopeSpans: []*tracepb.ScopeSpans{
				{
					Spans: []*tracepb.Span{
						{
							TraceId:           traceID,
							SpanId:            spanID,
							Name:              "process-request",
							StartTimeUnixNano: startNano,
							EndTimeUnixNano:   endNano,
							Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
						},
					},
				},
			},
		},
	}

	results := translator.TranslateResourceSpans("tenant-123", rs)

	if len(results) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(results))
	}

	tt := results[0]

	// Execution checks
	if tt.Execution.AgentID != "my-agent" {
		t.Errorf("expected agent_id=my-agent, got %s", tt.Execution.AgentID)
	}
	if tt.Execution.Trigger != "otlp" {
		t.Errorf("expected trigger=otlp, got %s", tt.Execution.Trigger)
	}
	if tt.Execution.Status != execution.StatusCompleted {
		t.Errorf("expected status=COMPLETED, got %s", tt.Execution.Status)
	}
	if tt.Execution.EndedAt == nil {
		t.Error("expected EndedAt to be set for completed execution")
	}
	if tt.Execution.Metadata["otel.source"] != "otlp" {
		t.Error("expected otel.source=otlp in metadata")
	}

	// Span checks
	if len(tt.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tt.Spans))
	}
	span := tt.Spans[0]
	if span.Name != "process-request" {
		t.Errorf("expected span name=process-request, got %s", span.Name)
	}
	if span.Status != execution.StatusCompleted {
		t.Errorf("expected span status=COMPLETED, got %s", span.Status)
	}
	if span.SpanType != execution.SpanKindCustom {
		t.Errorf("expected span type=CUSTOM, got %s", span.SpanType)
	}
}

func TestInferSpanType_GenAI(t *testing.T) {
	attrs := map[string]string{
		attrGenAISystem:       "openai",
		attrGenAIRequestModel: "gpt-4",
	}
	got := inferSpanType(attrs, tracepb.Span_SPAN_KIND_CLIENT)
	if got != execution.SpanKindModelInvocation {
		t.Errorf("expected MODEL_INVOCATION, got %s", got)
	}
}

func TestInferSpanType_ToolCall(t *testing.T) {
	attrs := map[string]string{
		attrToolName: "web_search",
	}
	got := inferSpanType(attrs, tracepb.Span_SPAN_KIND_CLIENT)
	if got != execution.SpanKindToolCall {
		t.Errorf("expected TOOL_CALL, got %s", got)
	}
}

func TestInferSpanType_ExplicitOverride(t *testing.T) {
	attrs := map[string]string{
		attrFact0SpanType: "HUMAN_APPROVAL",
		attrToolName:      "should-be-ignored",
	}
	got := inferSpanType(attrs, tracepb.Span_SPAN_KIND_INTERNAL)
	if got != execution.SpanKindHumanApproval {
		t.Errorf("expected HUMAN_APPROVAL, got %s", got)
	}
}

func TestExtractModelInvocation(t *testing.T) {
	attrs := map[string]string{
		attrGenAISystem:           "anthropic",
		attrGenAIRequestModel:     "claude-3-sonnet",
		attrGenAIPromptTokens:     "150",
		attrGenAICompletionTokens: "200",
		attrGenAITemperature:      "0.7",
	}
	detail := extractModelInvocation(attrs, 350)

	if detail.ModelProvider != "anthropic" {
		t.Errorf("expected provider=anthropic, got %s", detail.ModelProvider)
	}
	if detail.ModelName != "claude-3-sonnet" {
		t.Errorf("expected model=claude-3-sonnet, got %s", detail.ModelName)
	}
	if detail.PromptTokens != 150 {
		t.Errorf("expected prompt_tokens=150, got %d", detail.PromptTokens)
	}
	if detail.CompletionTokens != 200 {
		t.Errorf("expected completion_tokens=200, got %d", detail.CompletionTokens)
	}
	if detail.TotalTokens != 350 {
		t.Errorf("expected total_tokens=350 (auto-sum), got %d", detail.TotalTokens)
	}
	if detail.Temperature != 0.7 {
		t.Errorf("expected temperature=0.7, got %f", detail.Temperature)
	}
}

func TestDeriveExecutionID_Deterministic(t *testing.T) {
	id1 := deriveExecutionID("tenant-a", "trace-123")
	id2 := deriveExecutionID("tenant-a", "trace-123")
	if id1 != id2 {
		t.Errorf("expected deterministic IDs, got %s != %s", id1, id2)
	}

	id3 := deriveExecutionID("tenant-b", "trace-123")
	if id1 == id3 {
		t.Error("expected different IDs for different tenants")
	}
}

func TestFlattenAttributes(t *testing.T) {
	attrs := []*commonpb.KeyValue{
		{Key: "str", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}}},
		{Key: "int", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 42}}},
		{Key: "float", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 3.14}}},
		{Key: "bool", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}},
	}
	m := flattenAttributes(attrs)

	if m["str"] != "hello" {
		t.Errorf("expected str=hello, got %s", m["str"])
	}
	if m["int"] != "42" {
		t.Errorf("expected int=42, got %s", m["int"])
	}
	if m["float"] != "3.14" {
		t.Errorf("expected float=3.14, got %s", m["float"])
	}
	if m["bool"] != "true" {
		t.Errorf("expected bool=true, got %s", m["bool"])
	}
}

func TestMapStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name   string
		status *tracepb.Status
		ended  *time.Time
		want   execution.Status
	}{
		{"error", &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR}, &now, execution.StatusFailed},
		{"ok+ended", &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}, &now, execution.StatusCompleted},
		{"unset+ended", nil, &now, execution.StatusCompleted},
		{"unset+running", nil, nil, execution.StatusRunning},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapStatus(tc.status, tc.ended)
			if got != tc.want {
				t.Errorf("expected %s, got %s", tc.want, got)
			}
		})
	}
}
