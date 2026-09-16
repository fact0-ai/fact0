package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// strictBackend models the production acceptance contract and rejects child
// spans until their parents exist. It also detects changed payloads on replay.
type strictBackend struct {
	mu        sync.Mutex
	events    map[string]string
	spans     map[string]map[string]any
	starts    map[string]string
	requests  []capturedReq
	rejection string
}

func startStrictBackend(t *testing.T) (*httptest.Server, *strictBackend) {
	t.Helper()
	b := &strictBackend{events: map[string]string{}, spans: map[string]map[string]any{}, starts: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		defer b.mu.Unlock()
		b.requests = append(b.requests, capturedReq{r.Method, r.URL.Path, string(raw)})
		w.Header().Set("Content-Type", "application/json")
		if len(raw) > 4<<20 {
			b.rejection = "oversize request sent"
			w.WriteHeader(413)
			return
		}
		var data map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		_ = dec.Decode(&data)
		switch {
		case r.URL.Path == "/api/v1/executions":
			key, _ := data["idempotency_key"].(string)
			if key == "" {
				b.rejection = "missing idempotency key"
			}
			id := b.starts[key]
			if id == "" {
				id = fmt.Sprintf("exec_%d", len(b.starts)+1)
				b.starts[key] = id
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.URL.Path == "/v1/events/batch":
			events, _ := data["events"].([]any)
			for _, item := range events {
				event := item.(map[string]any)
				id, _ := event["id"].(string)
				body := jsonString(event)
				if prior := b.events[id]; prior != "" && prior != body {
					b.rejection = "changed audit payload on retry"
				}
				b.events[id] = body
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"accepted": len(events), "rejected": 0, "errors": []any{}})
		case strings.HasSuffix(r.URL.Path, "/spans"):
			spans, _ := data["spans"].([]any)
			for _, item := range spans {
				span := item.(map[string]any)
				id := span["id"].(string)
				if parent, ok := span["parent_span_id"].(string); ok && parent != "" && b.spans[parent] == nil {
					b.rejection = "missing parent " + parent
				}
				if old := b.spans[id]; old != nil {
					for _, field := range []string{"execution_id", "parent_span_id", "name", "span_type", "started_at"} {
						if jsonString(old[field]) != jsonString(span[field]) {
							b.rejection = "changed span identity: " + field
						}
					}
				}
				b.spans[id] = span
			}
			if b.rejection != "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"accepted_count": 0, "errors": []any{map[string]any{"code": "INVALID_PARENT", "message": b.rejection}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"accepted_count": len(spans), "errors": []any{}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, b
}
func queuedHooks(t *testing.T, cfg Config) []string {
	t.Helper()
	entries, err := os.ReadDir(spoolDir(cfg))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hook-") && strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, filepath.Join(spoolDir(cfg), e.Name()))
		}
	}
	return paths
}
func readEnvelope(t *testing.T, path string) hookEnvelope {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env hookEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&env); err != nil {
		t.Fatal(err)
	}
	return env
}
func drainForTest(cfg Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	drainHooks(ctx, NewFact0(cfg), cfg)
}

func TestDurableRawSessionCompleteAndOrdered(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	cfg.CaptureMode = CaptureRawMode
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	lines := `{"type":"user","message":{"content":[{"type":"text","text":"Do the work"}]}}
{"type":"assistant","uuid":"a1","message":{"id":"m1","model":"claude-test","content":[{"type":"text","text":"Planning ✓"}],"usage":{"output_tokens":10}}}
{"type":"assistant","uuid":"a2","message":{"id":"m2","content":[{"type":"tool_use","name":"Bash","input":{"n":9007199254740993}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"done"}]}}
{"type":"assistant","uuid":"a3","message":{"id":"m3","content":[{"type":"text","text":"Finished 東京"}],"usage":{"output_tokens":20}}}
`
	if err := os.WriteFile(transcript, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	fullOutput := strings.Repeat("東京🙂", 15000) + " output tail"
	tool := HookInput{SessionID: "raw-session", ToolName: "Bash", ToolUseID: "tool-1", ToolInput: json.RawMessage(`{"command":"printf 'hello'","number":9007199254740993}`), ToolResponse: json.RawMessage(`{"output":` + jsonString(fullOutput) + `,"n":9007199254740993,"exit_code":1}`), Error: strings.Repeat("full error 🙂 ", 200)}
	steps := []struct {
		event string
		in    HookInput
	}{{"session-start", HookInput{SessionID: "raw-session", Source: "startup"}}, {"prompt", HookInput{SessionID: "raw-session", Prompt: strings.Repeat("prompt λ ", 2000)}}, {"pre-tool", tool}, {"post-tool-failure", tool}, {"stop", HookInput{SessionID: "raw-session", TranscriptPath: transcript, LastAssistantMessage: stringPointer("Finished 東京")}}, {"session-end", HookInput{SessionID: "raw-session", Reason: "exit"}}}
	for _, step := range steps {
		if _, err := enqueueHook(cfg, step.event, step.in); err != nil {
			t.Fatal(err)
		}
	}
	// The transcript can advance after Stop: the queued snapshot must not change.
	_ = os.WriteFile(transcript, []byte(`{"type":"user","message":{"content":"later turn"}}`), 0600)
	drainForTest(cfg)
	if n := len(queuedHooks(t, cfg)); n != 0 {
		t.Fatalf("%d hooks not delivered", n)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rejection != "" {
		t.Fatal(b.rejection)
	}
	if len(b.starts) != 1 {
		t.Fatalf("executions=%d", len(b.starts))
	}
	if len(b.events) != 5 {
		t.Fatalf("events=%d", len(b.events))
	}
	var toolEvent, turnEvent map[string]any
	for _, raw := range b.events {
		var event map[string]any
		d := json.NewDecoder(strings.NewReader(raw))
		d.UseNumber()
		_ = d.Decode(&event)
		if event["action"] == "claude_code.tool.bash" {
			toolEvent = event
		}
		if event["action"] == "claude_code.turn.complete" {
			turnEvent = event
		}
	}
	if toolEvent == nil || turnEvent == nil {
		t.Fatal("missing tool or turn event")
	}
	metadata := toolEvent["metadata"].(map[string]any)
	if metadata["tool_response"].(map[string]any)["output"] != fullOutput {
		t.Fatal("tool output lost content")
	}
	if metadata["tool_input"].(map[string]any)["number"].(json.Number).String() != "9007199254740993" {
		t.Fatal("large integer rounded")
	}
	if metadata["error"] != tool.Error {
		t.Fatal("error clipped")
	}
	turnMD := turnEvent["metadata"].(map[string]any)
	if turnMD["response"] != "Planning ✓\nFinished 東京" || turnMD["capture_status"] != "complete" {
		t.Fatalf("turn metadata=%v", turnMD)
	}
	if len(turnMD["messages"].([]any)) != 3 {
		t.Fatal("assistant blocks lost")
	}
	if !strings.Contains(jsonString(turnMD["messages"]), "9007199254740993") {
		t.Fatal("transcript numeric content rounded")
	}
	var transitions []string
	for _, r := range b.requests {
		if strings.HasSuffix(r.Path, "/spans") {
			var batch struct {
				Spans []map[string]any `json:"spans"`
			}
			_ = json.Unmarshal([]byte(r.Body), &batch)
			for _, s := range batch.Spans {
				transitions = append(transitions, fmt.Sprint(s["name"], ":", s["status"]))
			}
		}
	}
	want := "turn:RUNNING,Bash:RUNNING,Bash:FAILED,turn:COMPLETED,model_invocation:COMPLETED"
	if strings.Join(transitions, ",") != want {
		t.Fatalf("span order %v", transitions)
	}
}

func TestMissingAndMalformedTranscriptVisible(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	cfg.CaptureMode = CaptureRawMode
	if _, err := enqueueHook(cfg, "stop", HookInput{SessionID: "missing", TranscriptPath: "/nonexistent/transcript"}); err != nil {
		t.Fatal(err)
	}
	drainForTest(cfg)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, raw := range b.events {
		if !strings.Contains(raw, `"capture_status":"unavailable"`) || !strings.Contains(raw, "transcript unreadable") {
			t.Fatalf("missing status: %s", raw)
		}
	}
	path := filepath.Join(t.TempDir(), "broken.jsonl")
	_ = os.WriteFile(path, []byte("{\"type\":\"assistant\",\"message\":{\"id\":\"m\",\"content\":[{\"type\":\"text\",\"text\":\"kept\"}]}}\n{broken\n"), 0600)
	stats, ok := ReadTurnFromTranscript(path)
	if !ok || stats.CaptureStatus != "partial" || stats.ResponseText != "kept" {
		t.Fatalf("malformed transcript result=%+v", stats)
	}
}

func TestCrashReplayRetainsExactIDsAndNumbers(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	cfg.CaptureMode = CaptureRawMode
	path, err := enqueueHook(cfg, "post-tool", HookInput{SessionID: "retry", ToolName: "Bash", ToolInput: json.RawMessage(`{"n":9007199254740993}`), ToolResponse: json.RawMessage(`{"output":"αβγ"}`)})
	if err != nil {
		t.Fatal(err)
	}
	env := readEnvelope(t, path)
	if err := prepareHook(context.Background(), NewFact0(cfg), cfg, &env); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvelope(cfg, path, &env); err != nil {
		t.Fatal(err)
	}
	for i := range env.Operations {
		if err := deliverOperation(context.Background(), NewFact0(cfg), cfg, &env.Operations[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate death after server acceptance but before progress/state was saved.
	drainForTest(cfg)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rejection != "" {
		t.Fatal(b.rejection)
	}
	if len(b.events) != 1 || len(b.spans) != 1 {
		t.Fatalf("duplicate records: %d events/%d spans", len(b.events), len(b.spans))
	}
	for _, raw := range b.events {
		if !strings.Contains(raw, "9007199254740993") {
			t.Fatal("replay rounded numeric metadata")
		}
	}
}

func TestExecutionStartRetryUsesPersistedKey(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	path, err := enqueueHook(cfg, "session-start", HookInput{SessionID: "startup-retry"})
	if err != nil {
		t.Fatal(err)
	}
	original := readEnvelope(t, path)
	first := original
	if err := prepareHook(context.Background(), NewFact0(cfg), cfg, &first); err != nil {
		t.Fatal(err)
	}
	// No prepared envelope or state persisted: a retried start still identifies
	// the same execution through the persisted original hook ID.
	second := original
	if err := prepareHook(context.Background(), NewFact0(cfg), cfg, &second); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.starts) != 1 || first.State.ExecutionID != second.State.ExecutionID {
		t.Fatal("execution retry duplicated execution")
	}
}

func TestConcurrentHooksPreserveAllState(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	if _, err := enqueueHook(cfg, "prompt", HookInput{SessionID: "parallel", Prompt: "parallel tools"}); err != nil {
		t.Fatal(err)
	}
	drainForTest(cfg)
	var wg sync.WaitGroup
	errCh := make(chan error, 40)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			in := HookInput{SessionID: "parallel", ToolName: "Read", ToolUseID: fmt.Sprint(n), ToolInput: json.RawMessage(`{"file_path":"/tmp/test"}`)}
			if _, err := enqueueHook(cfg, "pre-tool", in); err != nil {
				errCh <- err
			}
			in.ToolResponse = json.RawMessage(`{"content":"ok"}`)
			if _, err := enqueueHook(cfg, "post-tool", in); err != nil {
				errCh <- err
			}
			drainForTest(cfg)
		}(n)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	drainForTest(cfg)
	if n := len(queuedHooks(t, cfg)); n != 0 {
		t.Fatalf("queued=%d", n)
	}
	st, err := LoadState(cfg, "parallel")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.CompletedTools) != 20 || len(st.PendingSpans) != 1 {
		t.Fatalf("state lost: completed=%d pending=%d", len(st.CompletedTools), len(st.PendingSpans))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rejection != "" {
		t.Fatal(b.rejection)
	}
	if len(b.spans) != 21 || len(b.events) != 21 {
		t.Fatalf("records: %d spans %d events", len(b.spans), len(b.events))
	}
}

func TestOversizeAndSpoolCapacityNeverEvict(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	cfg.CaptureMode = CaptureRawMode
	cfg.RequestMaxBytes = 512
	path, err := enqueueHook(cfg, "prompt", HookInput{SessionID: "oversize", Prompt: strings.Repeat("🙂", 500)})
	if err != nil {
		t.Fatal(err)
	}
	drainForTest(cfg)
	env := readEnvelope(t, path)
	if !env.Permanent || !strings.Contains(env.LastError, "payload too large") {
		t.Fatalf("oversize not retained: %+v", env)
	}
	before, _ := os.ReadFile(path)
	drainForTest(cfg)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("permanent queue retried without config change")
	}
	b.mu.Lock()
	for _, r := range b.requests {
		if strings.HasSuffix(r.Path, "/spans") {
			t.Fatal("oversize sent")
		}
	}
	b.mu.Unlock()
	// Raising an intentionally lower client cap enables replay of retained data.
	cfg.RequestMaxBytes = 4 << 20
	drainForTest(cfg)
	if len(queuedHooks(t, cfg)) != 0 {
		t.Fatal("configuration recovery did not drain")
	}
	cfg.SpoolMaxBytes = 1000
	p, err := enqueueHook(cfg, "notify", HookInput{SessionID: "full", Message: "retained"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enqueueHook(cfg, "notify", HookInput{SessionID: "full", Message: strings.Repeat("x", 1500)}); err == nil {
		t.Fatal("expected explicit full queue error")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("existing entry evicted")
	}
	info, _ := os.Stat(p)
	dirInfo, _ := os.Stat(spoolDir(cfg))
	if info.Mode().Perm() != 0600 || dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("permissions file=%o dir=%o", info.Mode().Perm(), dirInfo.Mode().Perm())
	}
}

func TestHTTP200ItemRejectionIsNotSuccess(t *testing.T) {
	cases := []map[string]any{{"accepted": float64(0), "rejected": float64(1), "errors": []any{map[string]any{"index": 0, "message": "bad event"}}}, {"accepted_count": float64(1), "errors": []any{map[string]any{"entity_id": "s", "message": "bad span"}}}, {"accepted_count": float64(0)}}
	for _, resp := range cases {
		field := "accepted_count"
		if _, ok := resp["accepted"]; ok {
			field = "accepted"
		}
		if err := checkAcceptance(resp, field, 1); err == nil || !isPermanentDelivery(err) {
			t.Fatalf("rejection accepted: %v", resp)
		}
	}
}

func TestSessionEndMarksUnfinishedSpansPartial(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	for _, step := range []struct {
		event string
		in    HookInput
	}{{"prompt", HookInput{SessionID: "unfinished", Prompt: "work"}}, {"pre-tool", HookInput{SessionID: "unfinished", ToolName: "Read", ToolUseID: "unfinished-tool", ToolInput: json.RawMessage(`{"file_path":"/tmp/read"}`)}}, {"session-end", HookInput{SessionID: "unfinished", Reason: "cancelled"}}} {
		if _, err := enqueueHook(cfg, step.event, step.in); err != nil {
			t.Fatal(err)
		}
	}
	drainForTest(cfg)
	if len(queuedHooks(t, cfg)) != 0 {
		t.Fatal("unfinished spans failed delivery")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rejection != "" {
		t.Fatal(b.rejection)
	}
	if len(b.spans) != 2 {
		t.Fatalf("spans=%d", len(b.spans))
	}
	for _, span := range b.spans {
		md := span["metadata"].(map[string]any)
		if span["status"] != "CANCELLED" || md["capture_status"] != "partial" {
			t.Fatalf("unfinished span not marked: %v", span)
		}
	}
}

func TestSubagentTranscriptSnapshot(t *testing.T) {
	srv, b := startStrictBackend(t)
	cfg := testConfig(t, srv.URL)
	cfg.CaptureMode = CaptureRawMode
	if _, err := enqueueHook(cfg, "subagent-stop", HookInput{SessionID: "child", AgentID: "agent-1", AgentType: "Explore", AgentTranscriptPath: "testdata/transcript-turn.jsonl", LastAssistantMessage: stringPointer("The auth test passes now.")}); err != nil {
		t.Fatal(err)
	}
	drainForTest(cfg)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) != 1 {
		t.Fatalf("events=%d", len(b.events))
	}
	for _, body := range b.events {
		if !strings.Contains(body, "The auth test passes now.") || !strings.Contains(body, `"capture_status":"complete"`) {
			t.Fatal("subagent transcript content unavailable")
		}
	}
}

func TestSpanBatchesRespectRequestBoundary(t *testing.T) {
	cfg := Config{RequestMaxBytes: 512, transaction: &hookTransaction{}}
	spans := []map[string]any{{"id": "a", "metadata": map[string]string{"content": strings.Repeat("x", 300)}}, {"id": "b", "metadata": map[string]string{"content": strings.Repeat("y", 300)}}}
	if err := ingestSpans(context.Background(), nil, cfg, "exec", spans); err != nil {
		t.Fatal(err)
	}
	if len(cfg.transaction.Operations) != 2 {
		t.Fatal("span boundary batching failed")
	}
	for _, op := range cfg.transaction.Operations {
		body, _ := json.Marshal(operationBody(&op))
		if len(body) > 512 {
			t.Fatal("oversize batch created")
		}
	}
}

func stringPointer(s string) *string { return &s }
