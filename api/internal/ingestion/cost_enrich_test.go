package ingestion

import (
	"testing"

	"github.com/fact0-ai/fact0/internal/execution"
)

func miSpan(model string, cost float64, prompt, completion int32) *execution.Span {
	return &execution.Span{
		SpanType: "MODEL_INVOCATION",
		ModelInvocation: &execution.ModelInvocationDetail{
			ModelName:        model,
			ModelProvider:    "anthropic",
			CostUSD:          cost,
			PromptTokens:     prompt,
			CompletionTokens: completion,
		},
	}
}

func TestEnrichModelInvocationCost(t *testing.T) {
	// Tokens but no cost -> estimated from the pricing table (compile-time
	// defaults include claude-* prefixes, so any claude model resolves).
	s := miSpan("claude-sonnet-4-5", 0, 100_000, 10_000)
	enrichModelInvocationCost(s)
	if s.ModelInvocation.CostUSD <= 0 {
		t.Errorf("expected estimated cost > 0, got %v", s.ModelInvocation.CostUSD)
	}

	// Explicit client cost always wins.
	s = miSpan("claude-sonnet-4-5", 1.23, 100_000, 10_000)
	enrichModelInvocationCost(s)
	if s.ModelInvocation.CostUSD != 1.23 {
		t.Errorf("explicit cost should be preserved, got %v", s.ModelInvocation.CostUSD)
	}

	// No tokens -> untouched.
	s = miSpan("claude-sonnet-4-5", 0, 0, 0)
	enrichModelInvocationCost(s)
	if s.ModelInvocation.CostUSD != 0 {
		t.Errorf("no tokens should not produce cost, got %v", s.ModelInvocation.CostUSD)
	}

	// Non-model span -> no panic.
	enrichModelInvocationCost(&execution.Span{SpanType: "TOOL_CALL"})

	// Unknown model -> 0 (lookup miss), not an error.
	s = miSpan("totally-unknown-model-xyz", 0, 1000, 1000)
	enrichModelInvocationCost(s)
	if s.ModelInvocation.CostUSD != 0 {
		t.Errorf("unknown model should stay 0, got %v", s.ModelInvocation.CostUSD)
	}
}
