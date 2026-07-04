package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturedReq records one HTTP request received by the mock backend.
type capturedReq struct {
	Method string
	Path   string
	Body   string
}

// newMockBackend returns an httptest server that records every request and a
// pointer to the captured slice (guarded by mu).
func newMockBackend(t *testing.T) (*httptest.Server, *[]capturedReq, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	reqs := make([]capturedReq, 0, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, capturedReq{Method: r.Method, Path: r.URL.Path, Body: string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// StartExecution callers read an id back; a generic object is harmless
		// for the batch/spans endpoints too.
		_, _ = w.Write([]byte(`{"id":"exec_mock_123"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs, &mu
}

// testConfig builds a Config pointed at the mock backend, honoring the env vars
// the task specifies (FACT0_API_KEY / FACT0_BASE_URL) plus an isolated state dir.
func testConfig(t *testing.T, baseURL string) Config {
	t.Helper()
	t.Setenv("FACT0_API_KEY", "test")
	t.Setenv("FACT0_BASE_URL", baseURL)
	t.Setenv("FACT0_CC_STATE_DIR", t.TempDir())
	t.Setenv("FACT0_CC_DISABLED", "")
	// Pin hash mode: these tests assert the strictest privacy posture (no
	// raw text ships). Metadata-mode behavior is covered in capture_mode_test.go.
	t.Setenv("FACT0_CC_CAPTURE_MODE", "hash")
	t.Setenv("FACT0_CC_CAPTURE_RAW", "")
	cfg := LoadConfig()
	if cfg.APIKey != "test" {
		t.Fatalf("expected APIKey=test, got %q", cfg.APIKey)
	}
	if cfg.BaseURL != baseURL {
		t.Fatalf("expected BaseURL=%q, got %q", baseURL, cfg.BaseURL)
	}
	if cfg.CaptureMode != CaptureHash {
		t.Fatalf("expected hash capture mode, got %q", cfg.CaptureMode)
	}
	return cfg
}

const sampleSessionID = "8f3c1d2e-4a5b-6c7d-8e9f-0a1b2c3d4e5f"

// rawBashCommand is the sensitive string that must NOT leak when CaptureRaw is off.
const rawBashCommand = "ls -la"

func samplePostToolInput() HookInput {
	return HookInput{
		SessionID:     sampleSessionID,
		Cwd:           "/Users/dev/work/demo-service",
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolUseID:     "toolu_01A2b3C4d5E6f7G8h9I0jK1l",
		ToolInput:     json.RawMessage(`{"command":"` + rawBashCommand + `"}`),
		ToolResponse:  json.RawMessage(`{"output":"` + rawBashCommand + ` output here","exit_code":0}`),
	}
}

func samplePromptInput() HookInput {
	return HookInput{
		SessionID:     sampleSessionID,
		Cwd:           "/Users/dev/work/demo-service",
		HookEventName: "UserPromptSubmit",
		Prompt:        "List the files in the current directory and summarize the project layout.",
	}
}

func findReqs(reqs []capturedReq, pred func(capturedReq) bool) []capturedReq {
	var out []capturedReq
	for _, r := range reqs {
		if pred(r) {
			out = append(out, r)
		}
	}
	return out
}

// TestHandleUserPrompt verifies the prompt handler hits the batch endpoint and
// ships a sha256 of the prompt rather than the raw prompt text.
func TestHandleUserPrompt(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	in := samplePromptInput()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandleUserPrompt(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandleUserPrompt returned error: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	batch := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/v1/events/batch"
	})
	if len(batch) == 0 {
		t.Fatalf("expected a POST to /v1/events/batch, got requests: %+v", got)
	}

	body := batch[0].Body
	// Privacy: raw prompt text must NOT appear; its sha256 must.
	if strings.Contains(body, in.Prompt) {
		t.Errorf("raw prompt text leaked into request body: %s", body)
	}
	wantHash := Sha256Hex(in.Prompt)
	if !strings.Contains(body, wantHash) {
		t.Errorf("expected prompt sha256 %q in body, got: %s", wantHash, body)
	}
	if !strings.Contains(body, "prompt_sha256") {
		t.Errorf("expected prompt_sha256 field in body, got: %s", body)
	}
}

// TestHandlePostTool verifies the post-tool handler ingests a span (spans
// endpoint) and an audit event (batch endpoint), and that the raw shell command
// does not leak while its sha256 does.
func TestHandlePostTool(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	// Seed session state with an execution id so the spans endpoint is exercised.
	st := &SessionState{
		SessionID:    sampleSessionID,
		ExecutionID:  "exec_mock_123",
		PendingSpans: map[string]SpanStart{},
	}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	in := samplePostToolInput()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := HandlePostTool(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandlePostTool returned error: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	// (a) right paths: a spans ingest and a batch audit event.
	spans := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" &&
			strings.HasPrefix(r.Path, "/api/v1/executions/") &&
			strings.HasSuffix(r.Path, "/spans")
	})
	if len(spans) == 0 {
		t.Fatalf("expected a POST to /api/v1/executions/.../spans, got: %+v", got)
	}
	if !strings.Contains(spans[0].Path, "exec_mock_123") {
		t.Errorf("spans path missing execution id: %s", spans[0].Path)
	}

	batch := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/v1/events/batch"
	})
	if len(batch) == 0 {
		t.Fatalf("expected a POST to /v1/events/batch, got: %+v", got)
	}

	// (b) privacy: across EVERY request body, the raw command must not appear,
	// but its sha256 must appear at least once.
	wantHash := Sha256Hex(rawBashCommand)
	hashSeen := false
	for _, r := range got {
		if strings.Contains(r.Body, rawBashCommand) {
			t.Errorf("raw command %q leaked into %s %s body: %s", rawBashCommand, r.Method, r.Path, r.Body)
		}
		if strings.Contains(r.Body, wantHash) {
			hashSeen = true
		}
	}
	if !hashSeen {
		t.Errorf("expected command sha256 %q in some request body, bodies: %+v", wantHash, got)
	}
}

// TestHandlersNoPanicWithoutExecution ensures handlers do not panic when no
// prior execution/state exists (e.g. post-tool arriving with no session-start).
func TestHandlersNoPanicWithoutExecution(t *testing.T) {
	srv, _, _ := newMockBackend(t)
	cfg := testConfig(t, srv.URL)
	client := NewFact0(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// No SaveState beforehand: ExecutionID is empty, spans endpoint is skipped.
	if err := HandlePostTool(ctx, client, cfg, samplePostToolInput()); err != nil {
		t.Fatalf("HandlePostTool (no execution) returned error: %v", err)
	}
	if err := HandleUserPrompt(ctx, client, cfg, samplePromptInput()); err != nil {
		t.Fatalf("HandleUserPrompt returned error: %v", err)
	}
}
