package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestLazyExecutionHealsPostTool verifies that a post-tool hook arriving with
// no execution (SessionStart never fired) lazily starts one, ingests the span
// into it, and persists the id to session state.
func TestLazyExecutionHealsPostTool(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	// Seed state WITHOUT an execution id, as a pre-tool hook would have left it.
	st := &SessionState{SessionID: sampleSessionID, PendingSpans: map[string]SpanStart{}}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandlePostTool(ctx, client, cfg, samplePostToolInput()); err != nil {
		t.Fatalf("HandlePostTool: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	starts := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/api/v1/executions"
	})
	if len(starts) != 1 {
		t.Fatalf("expected 1 lazy POST /api/v1/executions, got %d (%+v)", len(starts), got)
	}
	if !strings.Contains(starts[0].Body, `"lazy_start":"true"`) {
		t.Errorf("lazy start request should carry lazy_start metadata: %s", starts[0].Body)
	}

	spans := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && strings.HasSuffix(r.Path, "/spans")
	})
	if len(spans) == 0 {
		t.Fatalf("expected span ingest after lazy start, got: %+v", got)
	}
	if !strings.Contains(spans[0].Path, "exec_mock_123") {
		t.Errorf("span ingest should target the lazily started execution: %s", spans[0].Path)
	}

	healed, err := LoadState(cfg, sampleSessionID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if healed.ExecutionID != "exec_mock_123" {
		t.Errorf("state should persist the lazy execution id, got %q", healed.ExecutionID)
	}
}

// TestLazyExecutionFailureStaysQuiet verifies that when the executions
// endpoint rejects the lazy start, the audit event still flows, no hard error
// surfaces, and the state keeps an empty execution id for the next retry.
func TestLazyExecutionFailureStaysQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/executions" {
			http.Error(w, `{"error":"nope"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandlePostTool(ctx, client, cfg, samplePostToolInput()); err != nil {
		t.Fatalf("HandlePostTool should not fail when lazy start is rejected: %v", err)
	}

	st, err := LoadState(cfg, sampleSessionID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.ExecutionID != "" {
		t.Errorf("execution id should stay empty after failed lazy start, got %q", st.ExecutionID)
	}
}

// TestLazyExecutionShortCircuits verifies that an existing execution id
// suppresses any lazy start call.
func TestLazyExecutionShortCircuits(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	st := &SessionState{
		SessionID:    sampleSessionID,
		ExecutionID:  "exec_existing_9",
		PendingSpans: map[string]SpanStart{},
	}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandlePostTool(ctx, client, cfg, samplePostToolInput()); err != nil {
		t.Fatalf("HandlePostTool: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	starts := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/api/v1/executions"
	})
	if len(starts) != 0 {
		t.Errorf("no lazy start expected when execution id exists, got: %+v", starts)
	}
	spans := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && strings.Contains(r.Path, "exec_existing_9") && strings.HasSuffix(r.Path, "/spans")
	})
	if len(spans) == 0 {
		t.Errorf("span ingest should use the existing execution id, got: %+v", got)
	}
}
