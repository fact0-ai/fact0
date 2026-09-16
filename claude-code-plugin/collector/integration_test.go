package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCollectorLiveAPI runs the real hook executable against a disposable,
// migrated Fact0 API/Postgres deployment. It intentionally writes fixture
// sessions; it is skipped unless both integration variables are supplied.
func TestCollectorLiveAPI(t *testing.T) {
	base := strings.TrimRight(os.Getenv("FACT0_INTEGRATION_URL"), "/")
	key := os.Getenv("FACT0_INTEGRATION_API_KEY")
	if base == "" || key == "" {
		t.Skip("set FACT0_INTEGRATION_URL and FACT0_INTEGRATION_API_KEY for the live API/Postgres gate")
	}
	binary := buildTestBinary(t)
	stateDir := t.TempDir()
	session := "collector-integration-" + strings.TrimPrefix(newSpanID(), "span_")
	env := cleanEnv("FACT0_BASE_URL="+base, "FACT0_API_KEY="+key, "FACT0_CC_STATE_DIR="+stateDir, "FACT0_CC_CAPTURE_MODE=raw")
	runCommand := func(event string, in HookInput, environment []string, wantExit int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, event)
		cmd.Env = environment
		if event != "flush" {
			raw, _ := json.Marshal(in)
			cmd.Stdin = bytes.NewReader(raw)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != wantExit {
			t.Fatalf("%s wanted exit %d: %v (%s)", event, wantExit, err, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("%s wrote hook output: %q", event, stdout.String())
		}
	}
	runHook := func(event string, in HookInput, environment []string) {
		t.Helper()
		runCommand(event, in, environment, 0)
	}
	get := func(path string) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: HTTP %d %s", path, resp.StatusCode, raw)
		}
		var body map[string]any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	transcript := filepath.Join(t.TempDir(), "turn.jsonl")
	transcriptLines := `{"type":"user","message":{"content":[{"type":"text","text":"integration prompt"}]}}
{"type":"assistant","uuid":"cc-first","message":{"id":"cc-message-first","model":"claude-fixture","content":[{"type":"text","text":"Planning 東京🙂"}],"usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"assistant","uuid":"cc-tool","message":{"id":"cc-message-tool","content":[{"type":"tool_use","name":"Bash","input":{"n":9007199254740993}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"done"}]}}
{"type":"assistant","uuid":"cc-last","message":{"id":"cc-message-last","model":"claude-fixture","content":[{"type":"text","text":"Finished café λ"}],"usage":{"input_tokens":20,"output_tokens":7}}}
`
	if err := os.WriteFile(transcript, []byte(transcriptLines), 0600); err != nil {
		t.Fatal(err)
	}
	prompt := strings.Repeat("Full prompt 東京🙂 ", 100)
	fullOutput := strings.Repeat("Full tool output café🙂 ", 10000) + "OUTPUT_END"
	tool := HookInput{SessionID: session, ToolName: "Bash", ToolUseID: "success-1", ToolInput: json.RawMessage(`{"command":"printf 'fixture'","n":9007199254740993}`), ToolResponse: json.RawMessage(`{"output":` + jsonString(fullOutput) + `,"n":9007199254740993,"exit_code":0}`)}
	failure := HookInput{SessionID: session, ToolName: "Read", ToolUseID: "failure-1", ToolInput: json.RawMessage(`{"file_path":"/tmp/fixture-file"}`), Error: strings.Repeat("unclipped error 東京🙂 ", 100)}
	steps := []struct {
		event string
		in    HookInput
	}{{"session-start", HookInput{SessionID: session, Source: "startup"}}, {"prompt", HookInput{SessionID: session, Prompt: prompt}}, {"pre-tool", tool}, {"post-tool", tool}, {"pre-tool", failure}, {"post-tool-failure", failure}, {"stop", HookInput{SessionID: session, TranscriptPath: transcript, LastAssistantMessage: stringPointer("Finished café λ")}}, {"session-end", HookInput{SessionID: session, Reason: "exit"}}}
	capturedStart := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	for i := range steps {
		steps[i].in.Timestamp = capturedStart.Add(time.Duration(i) * 2 * time.Second).Format(time.RFC3339Nano)
		runHook(steps[i].event, steps[i].in, env)
	}
	runHook("flush", HookInput{}, env)
	cfg := Config{StateDir: stateDir}
	if pending := queuedHooks(t, cfg); len(pending) > 0 {
		saved := readEnvelope(t, pending[0])
		t.Fatalf("live delivery retained %d hooks: %s", len(pending), saved.LastError)
	}
	result := get("/v1/events?session_id=" + url.QueryEscape(session) + "&page_size=1000")
	events, ok := result["events"].([]any)
	if !ok || len(events) != 6 {
		t.Fatalf("expected 6 session audit events, got %v", result["total"])
	}
	var executionID string
	seen := map[string]bool{}
	for _, item := range events {
		event := item.(map[string]any)
		action := event["action"].(string)
		seen[action] = true
		md := event["metadata"].(map[string]any)
		if id, _ := md["execution_id"].(string); id != "" {
			executionID = id
		}
		switch action {
		case "claude_code.prompt.submit":
			if md["prompt"] != prompt {
				t.Fatal("live prompt was clipped")
			}
		case "claude_code.tool.bash":
			response := md["tool_response"].(map[string]any)
			input := md["tool_input"].(map[string]any)
			if response["output"] != fullOutput {
				t.Fatal("live tool output was clipped")
			}
			if fmt.Sprint(input["n"]) != "9007199254740993" || fmt.Sprint(response["n"]) != "9007199254740993" {
				t.Fatal("live JSON number lost precision")
			}
		case "claude_code.tool.read":
			if md["error"] != failure.Error || event["outcome"] != "error" {
				t.Fatal("live error was lost or clipped")
			}
		case "claude_code.turn.complete":
			if md["response"] != "Planning 東京🙂\nFinished café λ" || md["capture_status"] != "complete" {
				t.Fatal("live assistant content incomplete")
			}
			if messages, ok := md["messages"].([]any); !ok || len(messages) != 3 || !strings.Contains(jsonString(messages), "9007199254740993") {
				t.Fatal("live assistant blocks or numeric content lost")
			}
		}
	}
	if executionID == "" || len(seen) != 6 {
		t.Fatal("missing session/execution correlation")
	}
	spansResult := get("/api/v1/executions/" + url.PathEscape(executionID) + "/spans")
	spans, ok := spansResult["spans"].([]any)
	if !ok || len(spans) != 4 {
		t.Fatalf("expected turn, 2 tools and model span, got %v", spansResult)
	}
	var turnID string
	for _, item := range spans {
		s := item.(map[string]any)
		if s["name"] == "turn" {
			turnID = fmt.Sprint(s["id"])
		}
		if s["status"] == "RUNNING" {
			t.Fatal("live span remained RUNNING")
		}
	}
	if turnID == "" {
		t.Fatal("missing parent turn span")
	}
	for _, item := range spans {
		s := item.(map[string]any)
		if s["name"] != "turn" && s["parent_span_id"] != turnID {
			t.Fatal("tool/model not attached to parent turn")
		}
	}
	execution := get("/api/v1/executions/" + url.PathEscape(executionID))
	recordedStart, err := time.Parse(time.RFC3339Nano, fmt.Sprint(execution["started_at"]))
	if err != nil {
		t.Fatal(err)
	}
	recordedEnd, err := time.Parse(time.RFC3339Nano, fmt.Sprint(execution["ended_at"]))
	if err != nil {
		t.Fatal(err)
	}
	wantEnd := capturedStart.Add(time.Duration(len(steps)-1) * 2 * time.Second)
	if !recordedStart.Equal(capturedStart) || !recordedEnd.Equal(wantEnd) {
		t.Fatalf("offline execution occurrence times lost: %s to %s", recordedStart, recordedEnd)
	}
	replay := get("/api/v1/executions/" + url.PathEscape(executionID) + "/replay")
	if fmt.Sprint(replay["duration_ms"]) != fmt.Sprint(wantEnd.Sub(capturedStart).Milliseconds()) {
		t.Fatalf("offline replay duration=%v", replay["duration_ms"])
	}
	for _, item := range spans {
		span := item.(map[string]any)
		if span["name"] == "model_invocation" {
			at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(span["started_at"]))
			if err != nil || !at.Equal(capturedStart.Add(12*time.Second)) {
				t.Fatalf("model span lost Stop time: %v", span["started_at"])
			}
		}
	}
	verified := get("/v1/verify")
	if verified["valid"] != true || verified["events_checked"] == nil {
		t.Fatalf("live verification failed: %v", verified)
	}

	// Exercise a real HTTP 200 per-item rejection through the executable.
	// The invalid parent is seeded only into this isolated test queue; the API
	// must reject the item and the collector must preserve it for inspection.
	rejectedDir := t.TempDir()
	rejectedCfg := Config{StateDir: rejectedDir}
	badSession := session + "-rejected"
	if err := SaveState(rejectedCfg, &SessionState{SessionID: badSession, ExecutionID: executionID, TurnSpanID: "missing-parent-" + session, PendingSpans: map[string]SpanStart{}}); err != nil {
		t.Fatal(err)
	}
	rejectedEnv := cleanEnv("FACT0_BASE_URL="+base, "FACT0_API_KEY="+key, "FACT0_CC_STATE_DIR="+rejectedDir, "FACT0_CC_CAPTURE_MODE=raw")
	runHook("post-tool", HookInput{SessionID: badSession, ToolName: "Read", ToolUseID: "rejected-tool", ToolInput: json.RawMessage(`{"file_path":"/tmp/rejected"}`)}, rejectedEnv)
	runCommand("flush", HookInput{}, rejectedEnv, 1)
	pending := queuedHooks(t, rejectedCfg)
	if len(pending) != 1 {
		t.Fatalf("HTTP 200 rejected hook queue length=%d", len(pending))
	}
	saved := readEnvelope(t, pending[0])
	if !saved.Permanent || !strings.Contains(saved.LastError, "batch item rejection") {
		t.Fatalf("HTTP 200 rejection not retained: %s", saved.LastError)
	}
	t.Log("real binary/API/Postgres: full session, Unicode, large numbers, parent projections, verification, and per-item rejection passed")
}
