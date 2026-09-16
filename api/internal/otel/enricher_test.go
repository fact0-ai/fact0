package otel

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/fact0-ai/fact0/internal/execution"
)

func TestEnrich_DatabaseSpan(t *testing.T) {
	enricher := NewEnricher()

	span := &execution.Span{
		SpanType: execution.SpanKindCustom,
		Metadata: map[string]string{
			SemConvDBSystem:    "postgresql",
			SemConvDBName:      "mydb",
			SemConvDBStatement: "SELECT * FROM users WHERE id = $1",
		},
	}
	otelSpan := &tracepb.Span{}

	enricher.Enrich(span, otelSpan)

	if span.SpanType != execution.SpanKindToolCall {
		t.Errorf("expected TOOL_CALL, got %s", span.SpanType)
	}
	if span.ToolCall == nil {
		t.Fatal("expected ToolCall to be set")
	}
	if span.ToolCall.ToolName != "db:postgresql" {
		t.Errorf("expected tool name=db:postgresql, got %s", span.ToolCall.ToolName)
	}
	if span.ToolCall.ToolVersion != "mydb" {
		t.Errorf("expected tool version=mydb, got %s", span.ToolCall.ToolVersion)
	}
	if span.ToolCall.Input == nil {
		t.Fatal("expected Input payload for SQL statement")
	}
}

func TestEnrich_HTTPSpan(t *testing.T) {
	enricher := NewEnricher()

	span := &execution.Span{
		SpanType: execution.SpanKindCustom,
		Metadata: map[string]string{
			SemConvHTTPMethod:     "POST",
			SemConvHTTPURL:        "https://api.openai.com/v1/chat/completions",
			SemConvHTTPStatusCode: "200",
		},
	}
	otelSpan := &tracepb.Span{}

	enricher.Enrich(span, otelSpan)

	if span.SpanType != execution.SpanKindToolCall {
		t.Errorf("expected TOOL_CALL, got %s", span.SpanType)
	}
	if span.ToolCall == nil {
		t.Fatal("expected ToolCall to be set")
	}
	if span.ToolCall.ToolName != "POST https://api.openai.com/v1/chat/completions" {
		t.Errorf("unexpected tool name: %s", span.ToolCall.ToolName)
	}
}

func TestEnrich_GenAIEvents(t *testing.T) {
	enricher := NewEnricher()

	span := &execution.Span{
		SpanType: execution.SpanKindModelInvocation,
		Metadata: map[string]string{},
		ModelInvocation: &execution.ModelInvocationDetail{
			ModelName:     "gpt-4",
			ModelProvider: "openai",
		},
	}

	otelSpan := &tracepb.Span{
		Events: []*tracepb.Span_Event{
			{
				Name: "gen_ai.content.prompt",
				Attributes: []*commonpb.KeyValue{
					{Key: "gen_ai.prompt", Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "What is the meaning of life?"},
					}},
				},
			},
			{
				Name: "gen_ai.content.completion",
				Attributes: []*commonpb.KeyValue{
					{Key: "gen_ai.completion", Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "42"},
					}},
				},
			},
		},
	}

	enricher.Enrich(span, otelSpan)

	if span.ModelInvocation.Prompt == nil {
		t.Fatal("expected Prompt to be set from span event")
	}
	if span.ModelInvocation.Prompt.Inline != "What is the meaning of life?" {
		t.Errorf("unexpected prompt: %v", span.ModelInvocation.Prompt.Inline)
	}
	if span.ModelInvocation.Completion == nil {
		t.Fatal("expected Completion to be set from span event")
	}
	if span.ModelInvocation.Completion.Inline != "42" {
		t.Errorf("unexpected completion: %v", span.ModelInvocation.Completion.Inline)
	}
}

func TestEnrich_ExceptionEvent(t *testing.T) {
	enricher := NewEnricher()

	span := &execution.Span{
		SpanType: execution.SpanKindCustom,
		Status:   execution.StatusCompleted,
		Metadata: map[string]string{},
	}

	otelSpan := &tracepb.Span{
		Events: []*tracepb.Span_Event{
			{
				Name: "exception",
				Attributes: []*commonpb.KeyValue{
					{Key: SemConvExceptionType, Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "ValueError"},
					}},
					{Key: SemConvExceptionMessage, Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "invalid input"},
					}},
					{Key: SemConvExceptionStacktrace, Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "Traceback:\n  File main.py, line 42"},
					}},
				},
			},
		},
	}

	enricher.Enrich(span, otelSpan)

	if span.Error == nil {
		t.Fatal("expected Error to be set from exception event")
	}
	if span.Error.Code != "ValueError" {
		t.Errorf("expected error code=ValueError, got %s", span.Error.Code)
	}
	if span.Error.Message != "invalid input" {
		t.Errorf("expected error message=invalid input, got %s", span.Error.Message)
	}
	if span.Error.StackTrace == "" {
		t.Error("expected stack trace to be set")
	}
	if span.Status != execution.StatusFailed {
		t.Errorf("expected status=FAILED after exception, got %s", span.Status)
	}
}

func TestEnrich_SkipsAlreadyClassifiedSpans(t *testing.T) {
	enricher := NewEnricher()

	// A span already classified as MODEL_INVOCATION should NOT be
	// reclassified as TOOL_CALL even if db.system is present.
	span := &execution.Span{
		SpanType: execution.SpanKindModelInvocation,
		Metadata: map[string]string{
			SemConvDBSystem: "postgresql",
		},
		ModelInvocation: &execution.ModelInvocationDetail{
			ModelName: "gpt-4",
		},
	}
	otelSpan := &tracepb.Span{}

	enricher.Enrich(span, otelSpan)

	if span.SpanType != execution.SpanKindModelInvocation {
		t.Errorf("expected MODEL_INVOCATION preserved, got %s", span.SpanType)
	}
	if span.ToolCall != nil {
		t.Error("expected ToolCall to remain nil for already-classified span")
	}
}

func TestEnrich_ExistingErrorNotOverwritten(t *testing.T) {
	enricher := NewEnricher()

	span := &execution.Span{
		SpanType: execution.SpanKindCustom,
		Metadata: map[string]string{},
		Error: &execution.SpanError{
			Code:    "OTEL_ERROR",
			Message: "original error",
		},
	}

	otelSpan := &tracepb.Span{
		Events: []*tracepb.Span_Event{
			{
				Name: "exception",
				Attributes: []*commonpb.KeyValue{
					{Key: SemConvExceptionMessage, Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: "should not overwrite"},
					}},
				},
			},
		},
	}

	enricher.Enrich(span, otelSpan)

	if span.Error.Message != "original error" {
		t.Errorf("expected original error preserved, got %s", span.Error.Message)
	}
}
