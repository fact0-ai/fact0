package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// spansFromReqs decodes every captured request to a spans-ingest endpoint and
// returns the flattened list of span objects across all such requests.
func spansFromReqs(t *testing.T, reqs []capturedReq) []map[string]any {
	t.Helper()
	var spans []map[string]any
	for _, r := range reqs {
		if r.Method != "POST" ||
			!strings.HasPrefix(r.Path, "/api/v1/executions/") ||
			!strings.HasSuffix(r.Path, "/spans") {
			continue
		}
		var body struct {
			Spans []map[string]any `json:"spans"`
		}
		if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
			t.Fatalf("decode spans body %q: %v", r.Body, err)
		}
		spans = append(spans, body.Spans...)
	}
	return spans
}

func findSpan(spans []map[string]any, pred func(map[string]any) bool) (map[string]any, bool) {
	for _, s := range spans {
		if pred(s) {
			return s, true
		}
	}
	return nil, false
}

// TestToolCallNestsUnderTurnSpan asserts that, after a UserPromptSubmit opens a
// turn span, a subsequent PostToolUse ingests a TOOL_CALL span whose
// parent_span_id equals the turn span id persisted in session state.
func TestToolCallNestsUnderTurnSpan(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	// Seed an execution id so the spans endpoint is exercised.
	if err := SaveState(cfg, &SessionState{
		SessionID:    sampleSessionID,
		ExecutionID:  "exec_mock_123",
		PendingSpans: map[string]SpanStart{},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandleUserPrompt(ctx, client, cfg, samplePromptInput()); err != nil {
		t.Fatalf("HandleUserPrompt: %v", err)
	}

	// The turn span id is now persisted in state.
	st, err := LoadState(cfg, sampleSessionID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	turnID := st.TurnSpanID
	if turnID == "" {
		t.Fatalf("expected TurnSpanID to be set after HandleUserPrompt")
	}

	if err := HandlePostTool(ctx, client, cfg, samplePostToolInput()); err != nil {
		t.Fatalf("HandlePostTool: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	spans := spansFromReqs(t, got)
	toolSpan, ok := findSpan(spans, func(s map[string]any) bool {
		return s["span_type"] == "TOOL_CALL"
	})
	if !ok {
		t.Fatalf("expected a TOOL_CALL span among ingested spans: %+v", spans)
	}
	if parent, _ := toolSpan["parent_span_id"].(string); parent != turnID {
		t.Errorf("TOOL_CALL parent_span_id = %q, want turn span id %q", parent, turnID)
	}
}

// TestHandleStopIngestsTurnSpan asserts that HandleStop ingests the open turn
// span as a CUSTOM span and clears TurnSpanID from state.
func TestHandleStopIngestsTurnSpan(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	if err := SaveState(cfg, &SessionState{
		SessionID:    sampleSessionID,
		ExecutionID:  "exec_mock_123",
		PendingSpans: map[string]SpanStart{},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandleUserPrompt(ctx, client, cfg, samplePromptInput()); err != nil {
		t.Fatalf("HandleUserPrompt: %v", err)
	}
	preStop, err := LoadState(cfg, sampleSessionID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	turnID := preStop.TurnSpanID
	if turnID == "" {
		t.Fatalf("expected TurnSpanID set before stop")
	}

	if err := HandleStop(ctx, client, cfg, HookInput{SessionID: sampleSessionID}); err != nil {
		t.Fatalf("HandleStop: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	spans := spansFromReqs(t, got)
	turnSpan, ok := findSpan(spans, func(s map[string]any) bool {
		return s["name"] == "turn" && s["span_type"] == "CUSTOM"
	})
	if !ok {
		t.Fatalf("expected a CUSTOM \"turn\" span ingested on stop: %+v", spans)
	}
	if id, _ := turnSpan["id"].(string); id != turnID {
		t.Errorf("turn span id = %q, want %q", id, turnID)
	}

	// TurnSpanID must be cleared after stop.
	postStop, err := LoadState(cfg, sampleSessionID)
	if err != nil {
		t.Fatalf("LoadState after stop: %v", err)
	}
	if postStop.TurnSpanID != "" {
		t.Errorf("expected TurnSpanID cleared after HandleStop, got %q", postStop.TurnSpanID)
	}
	if _, stillOpen := postStop.PendingSpans["__turn__"]; stillOpen {
		t.Errorf("expected __turn__ pending span removed after HandleStop")
	}
}

// TestMaybeModelInvocationSpan covers the cost-bearing and absent cases.
func TestMaybeModelInvocationSpan(t *testing.T) {
	// (c1) cost present -> MODEL_INVOCATION span with cost_usd set.
	in := HookInput{
		SessionID:    sampleSessionID,
		TotalCostUSD: 0.0123,
		Usage:        json.RawMessage(`{"input_tokens":10,"output_tokens":20,"model":"claude-3-5-sonnet"}`),
	}
	span, ok := MaybeModelInvocationSpan(in, "exec_mock_123", "turn_span_1")
	if !ok {
		t.Fatalf("expected MODEL_INVOCATION span when TotalCostUSD>0")
	}
	if span["span_type"] != "MODEL_INVOCATION" {
		t.Errorf("span_type = %v, want MODEL_INVOCATION", span["span_type"])
	}
	mi, _ := span["model_invocation"].(map[string]any)
	if mi == nil {
		t.Fatalf("expected model_invocation block, got: %+v", span)
	}
	cost, ok := mi["cost_usd"].(float64)
	if !ok || cost != 0.0123 {
		t.Errorf("cost_usd = %v (ok=%v), want 0.0123", mi["cost_usd"], ok)
	}
	if span["execution_id"] != "exec_mock_123" {
		t.Errorf("execution_id = %v, want exec_mock_123", span["execution_id"])
	}
	if span["parent_span_id"] != "turn_span_1" {
		t.Errorf("parent_span_id = %v, want turn_span_1", span["parent_span_id"])
	}

	// (c2) no cost and no usage -> (nil,false).
	if s, ok := MaybeModelInvocationSpan(HookInput{SessionID: sampleSessionID}, "exec_mock_123", "turn_span_1"); ok || s != nil {
		t.Errorf("expected (nil,false) when no cost/usage present, got (%+v, %v)", s, ok)
	}
}
