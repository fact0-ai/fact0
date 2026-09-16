package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReadTurnFromTranscript covers the three parsing traps: turn boundary at
// the last REAL user prompt (tool_result lines are also type "user"),
// per-message-id token dedup (one message spans multiple JSONL lines), and
// last-text-wins response extraction.
func TestReadTurnFromTranscript(t *testing.T) {
	stats, ok := ReadTurnFromTranscript("testdata/transcript-turn.jsonl")
	if !ok {
		t.Fatal("expected turn stats from fixture")
	}
	if stats.ResponseText != "The auth test passes now." {
		t.Errorf("response = %q, want last turn's final text", stats.ResponseText)
	}
	if strings.Contains(stats.ResponseText, "old turn") {
		t.Errorf("response leaked previous turn text: %q", stats.ResponseText)
	}
	// msg_a appears on TWO lines with output 200 — must count once: 200+150.
	if stats.OutputTokens != 350 {
		t.Errorf("output tokens = %d, want 350 (deduped by message id)", stats.OutputTokens)
	}
	// Last call's effective context: 8 + 410000 + 500.
	if stats.InputTokens != 410508 {
		t.Errorf("input tokens = %d, want 410508", stats.InputTokens)
	}
	if stats.Model != "claude-fable-5" {
		t.Errorf("model = %q", stats.Model)
	}
}

func TestReadTurnFromTranscript_MissingFile(t *testing.T) {
	if _, ok := ReadTurnFromTranscript("testdata/does-not-exist.jsonl"); ok {
		t.Error("expected ok=false for missing transcript")
	}
	if _, ok := ReadTurnFromTranscript(""); ok {
		t.Error("expected ok=false for empty path")
	}
}

// TestHandleStopEmitsTurnComplete verifies the Stop hook ships a
// turn.complete audit event with the response preview (metadata mode) and a
// MODEL_INVOCATION span with transcript-derived tokens.
func TestHandleStopEmitsTurnComplete(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := metadataConfig(t, srv.URL)
	client := NewFact0(cfg)

	st := &SessionState{
		SessionID:   sampleSessionID,
		ExecutionID: "exec_mock_123",
		TurnSpanID:  "span_turn_1",
		PendingSpans: map[string]SpanStart{
			"__turn__": {SpanID: "span_turn_1", Name: "turn", StartedAt: nowRFC3339()},
		},
	}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	in := HookInput{
		SessionID:      sampleSessionID,
		HookEventName:  "Stop",
		TranscriptPath: "testdata/transcript-turn.jsonl",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := HandleStop(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandleStop: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	batches := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/v1/events/batch"
	})
	turnComplete := false
	for _, b := range batches {
		if strings.Contains(b.Body, "claude_code.turn.complete") {
			turnComplete = true
			if !strings.Contains(b.Body, "The auth test passes now.") {
				t.Errorf("metadata mode should ship the response preview: %s", b.Body)
			}
			if !strings.Contains(b.Body, `"output_tokens":350`) {
				t.Errorf("turn.complete should carry deduped token counts: %s", b.Body)
			}
		}
	}
	if !turnComplete {
		t.Fatalf("expected a claude_code.turn.complete event, got: %+v", got)
	}

	spanReqs := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && strings.HasSuffix(r.Path, "/spans") &&
			strings.Contains(r.Body, "MODEL_INVOCATION")
	})
	if len(spanReqs) == 0 {
		t.Fatalf("expected a MODEL_INVOCATION span ingest, got: %+v", got)
	}
	if !strings.Contains(spanReqs[0].Body, `"completion_tokens":350`) ||
		!strings.Contains(spanReqs[0].Body, `"parent_span_id":"span_turn_1"`) {
		t.Errorf("model invocation span wrong: %s", spanReqs[0].Body)
	}
}

// TestHandleStopHashModeOmitsResponse verifies hash mode ships only the
// digest, never the response text.
func TestHandleStopHashModeOmitsResponse(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL) // pinned to hash mode
	client := NewFact0(cfg)

	st := &SessionState{SessionID: sampleSessionID, ExecutionID: "exec_mock_123", PendingSpans: map[string]SpanStart{}}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	in := HookInput{
		SessionID:      sampleSessionID,
		HookEventName:  "Stop",
		TranscriptPath: "testdata/transcript-turn.jsonl",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := HandleStop(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandleStop: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()
	for _, r := range got {
		if strings.Contains(r.Body, "The auth test passes now.") {
			t.Errorf("hash mode leaked response text into %s: %s", r.Path, r.Body)
		}
	}
}

func TestTranscriptSameUUIDRetainsChangedBlocksAndUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updated.jsonl")
	lines := `{"type":"user","message":{"content":"prompt"}}
{"type":"assistant","uuid":"same","message":{"id":"m","content":[{"type":"text","text":"first"}],"usage":{"output_tokens":1}}}
{"type":"assistant","uuid":"same","message":{"id":"m","content":[{"type":"text","text":"second"}],"usage":{"output_tokens":2}}}
{"type":"assistant","uuid":"same","message":{"id":"m","content":[{"type":"text","text":"second"}],"usage":{"output_tokens":3}}}
`
	if err := os.WriteFile(path, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	stats, ok := ReadTurnFromTranscript(path)
	if !ok || stats.ResponseText != "first\nsecond" || stats.OutputTokens != 3 || len(stats.Messages) != 2 {
		t.Fatalf("lost updated transcript content: %+v", stats)
	}
}

func TestRealUserPromptBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{{`"prompt"`, true}, {`[{"type":"text","text":"prompt"}]`, true}, {`[{"type":"image","source":{"type":"base64","data":"fixture"}}]`, true}, {`[{"type":"tool_result","content":"output"}]`, false}, {`[]`, false}} {
		if got := realUserPrompt(json.RawMessage(tc.raw)); got != tc.want {
			t.Errorf("prompt boundary %s=%v", tc.raw, got)
		}
	}
}
