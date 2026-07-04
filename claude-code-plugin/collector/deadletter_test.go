package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// newFailingBackend returns a server that rejects every request with 400,
// which the SDK treats as a permanent error (no retries), keeping tests fast.
func newFailingBackend(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func listDeadLetters(t *testing.T, cfg Config) []string {
	t.Helper()
	entries, err := os.ReadDir(deadLetterDir(cfg))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if isDeadLetterFile(e) {
			names = append(names, e.Name())
		}
	}
	return names
}

func sampleAuditEvent(cfg Config) fact0.AuditEventInput {
	return fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   "claude_code.prompt.submit",
		Resource: fact0.Resource{ID: sampleSessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: map[string]interface{}{"prompt_len": 42},
	}
}

// TestAuditDeadLetterAndReplay verifies that a failed audit send is queued to
// the dead-letter dir with its original timestamp, and that a later replay
// against a healthy backend delivers it and removes the file.
func TestAuditDeadLetterAndReplay(t *testing.T) {
	bad := newFailingBackend(t)
	cfg := testConfig(t, bad.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send fails -> queued, not lost: auditLog reports success (nil).
	if err := auditLog(ctx, NewFact0(cfg), cfg, sampleAuditEvent(cfg)); err != nil {
		t.Fatalf("auditLog should return nil after dead-lettering, got: %v", err)
	}

	names := listDeadLetters(t, cfg)
	if len(names) != 1 {
		t.Fatalf("expected 1 dead letter, got %d (%v)", len(names), names)
	}
	if got := CountDeadLetters(cfg); got != 1 {
		t.Fatalf("CountDeadLetters = %d, want 1", got)
	}
	data, err := os.ReadFile(filepath.Join(deadLetterDir(cfg), names[0]))
	if err != nil {
		t.Fatalf("read dead letter: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"audit"`) {
		t.Errorf("dead letter missing audit kind: %s", data)
	}
	if !strings.Contains(string(data), `"timestamp"`) {
		t.Errorf("dead letter should preserve the original event timestamp: %s", data)
	}

	// Replay against a healthy backend delivers the original event.
	good, reqs, mu := newMockBackend(t)
	goodCfg := cfg
	goodCfg.BaseURL = good.URL
	ReplayDeadLetters(ctx, NewFact0(goodCfg), goodCfg)

	if got := CountDeadLetters(cfg); got != 0 {
		t.Fatalf("expected 0 dead letters after replay, got %d", got)
	}
	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()
	batch := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/v1/events/batch"
	})
	if len(batch) != 1 {
		t.Fatalf("expected 1 replayed POST /v1/events/batch, got: %+v", got)
	}
	if !strings.Contains(batch[0].Body, "claude_code.prompt.submit") {
		t.Errorf("replayed body missing original action: %s", batch[0].Body)
	}
}

// TestSpansAndEndExecutionDeadLetter verifies span-ingest and end-execution
// failures are queued and replayed against the right endpoints, oldest first.
func TestSpansAndEndExecutionDeadLetter(t *testing.T) {
	bad := newFailingBackend(t)
	cfg := testConfig(t, bad.URL)
	client := NewFact0(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	span := map[string]any{"id": "span_abc", "span_type": "TOOL_CALL", "name": "Bash"}
	if err := ingestSpans(ctx, client, cfg, "exec_mock_123", []map[string]any{span}); err != nil {
		t.Fatalf("ingestSpans should return nil after dead-lettering, got: %v", err)
	}
	if err := endExecution(ctx, client, cfg, "exec_mock_123", "COMPLETED"); err != nil {
		t.Fatalf("endExecution should return nil after dead-lettering, got: %v", err)
	}
	if got := CountDeadLetters(cfg); got != 2 {
		t.Fatalf("expected 2 dead letters, got %d", got)
	}

	good, reqs, mu := newMockBackend(t)
	goodCfg := cfg
	goodCfg.BaseURL = good.URL
	ReplayDeadLetters(ctx, NewFact0(goodCfg), goodCfg)

	if got := CountDeadLetters(cfg); got != 0 {
		t.Fatalf("expected 0 dead letters after replay, got %d", got)
	}
	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 replayed requests, got: %+v", got)
	}
	// Oldest first: the span ingest was queued before the end-execution.
	if !strings.HasSuffix(got[0].Path, "/spans") {
		t.Errorf("first replayed request should be span ingest, got %s", got[0].Path)
	}
	if !strings.HasSuffix(got[1].Path, "/end") {
		t.Errorf("second replayed request should be end-execution, got %s", got[1].Path)
	}
}

// TestReplayStopsOnFirstFailure verifies that when the backend is still down,
// replay bumps the first letter's attempt count and leaves the rest untouched.
func TestReplayStopsOnFirstFailure(t *testing.T) {
	bad := newFailingBackend(t)
	cfg := testConfig(t, bad.URL)
	client := NewFact0(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := auditLog(ctx, client, cfg, sampleAuditEvent(cfg)); err != nil {
		t.Fatalf("auditLog: %v", err)
	}
	if err := endExecution(ctx, client, cfg, "exec_mock_123", "COMPLETED"); err != nil {
		t.Fatalf("endExecution: %v", err)
	}

	ReplayDeadLetters(ctx, client, cfg)

	names := listDeadLetters(t, cfg)
	if len(names) != 2 {
		t.Fatalf("expected both dead letters to remain, got %d (%v)", len(names), names)
	}
	data, err := os.ReadFile(filepath.Join(deadLetterDir(cfg), names[0]))
	if err != nil {
		t.Fatalf("read first dead letter: %v", err)
	}
	if !strings.Contains(string(data), `"attempts":1`) {
		t.Errorf("first dead letter should have attempts=1 after failed replay: %s", data)
	}
	second, err := os.ReadFile(filepath.Join(deadLetterDir(cfg), names[1]))
	if err != nil {
		t.Fatalf("read second dead letter: %v", err)
	}
	if !strings.Contains(string(second), `"attempts":0`) {
		t.Errorf("second dead letter should be untouched (attempts=0): %s", second)
	}
}

// TestAbandonAfterMaxAttempts verifies a letter that keeps failing is renamed
// *.abandoned and never retried again.
func TestAbandonAfterMaxAttempts(t *testing.T) {
	bad := newFailingBackend(t)
	cfg := testConfig(t, bad.URL)
	client := NewFact0(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ev := sampleAuditEvent(cfg)
	ev.Timestamp = nowRFC3339()
	dl := &DeadLetter{Kind: dlKindAudit, Audit: &ev, Attempts: maxDeliveryAttempts - 1}
	if err := SaveDeadLetter(cfg, dl); err != nil {
		t.Fatalf("SaveDeadLetter: %v", err)
	}

	ReplayDeadLetters(ctx, client, cfg)

	if got := CountDeadLetters(cfg); got != 0 {
		t.Fatalf("expected letter to be abandoned, %d still pending", got)
	}
	entries, err := os.ReadDir(deadLetterDir(cfg))
	if err != nil {
		t.Fatalf("read dead-letter dir: %v", err)
	}
	abandoned := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), dlAbandoned) {
			abandoned++
		}
	}
	if abandoned != 1 {
		t.Fatalf("expected 1 .abandoned file, got %d", abandoned)
	}
}
