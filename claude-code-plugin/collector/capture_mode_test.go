package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// metadataConfig builds a Config in metadata mode pointed at the mock backend.
func metadataConfig(t *testing.T, baseURL string) Config {
	t.Helper()
	cfg := testConfig(t, baseURL)
	t.Setenv("FACT0_CC_CAPTURE_MODE", "metadata")
	cfg = LoadConfig()
	if cfg.CaptureMode != CaptureMetadata {
		t.Fatalf("expected metadata capture mode, got %q", cfg.CaptureMode)
	}
	return cfg
}

// TestCaptureModeResolution covers env resolution: default, legacy flag,
// explicit mode precedence, and invalid values.
func TestCaptureModeResolution(t *testing.T) {
	cases := []struct {
		name, mode, legacyRaw, want string
	}{
		{"default is metadata", "", "", CaptureMetadata},
		{"legacy raw flag", "", "1", CaptureRawMode},
		{"explicit hash beats legacy", "hash", "1", CaptureHash},
		{"explicit raw", "raw", "", CaptureRawMode},
		{"invalid falls back to metadata", "everything", "1", CaptureMetadata},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FACT0_CC_CAPTURE_MODE", tc.mode)
			t.Setenv("FACT0_CC_CAPTURE_RAW", tc.legacyRaw)
			if got := captureModeFromEnv(); got != tc.want {
				t.Errorf("mode=%q raw=%q: got %q, want %q", tc.mode, tc.legacyRaw, got, tc.want)
			}
		})
	}
}

// TestMetadataModeBash verifies that in metadata mode the command text ships
// (resource name + tool_input.command) while the tool OUTPUT text stays hashed.
func TestMetadataModeBash(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := metadataConfig(t, srv.URL)
	client := NewFact0(cfg)

	st := &SessionState{SessionID: sampleSessionID, ExecutionID: "exec_mock_123", PendingSpans: map[string]SpanStart{}}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	const outputText = "secret-looking output body"
	in := samplePostToolInput()
	in.ToolResponse = json.RawMessage(`{"output":"` + outputText + `","exit_code":0}`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := HandlePostTool(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandlePostTool: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	cmdSeen, outputLeaked := false, false
	for _, r := range got {
		if strings.Contains(r.Body, rawBashCommand) {
			cmdSeen = true
		}
		if strings.Contains(r.Body, outputText) {
			outputLeaked = true
		}
	}
	if !cmdSeen {
		t.Errorf("metadata mode should ship the command text, bodies: %+v", got)
	}
	if outputLeaked {
		t.Errorf("metadata mode must NOT ship tool output text, bodies: %+v", got)
	}
}

// TestMetadataModeEdit verifies file paths ship but file CONTENTS stay hashed.
func TestMetadataModeEdit(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := metadataConfig(t, srv.URL)
	client := NewFact0(cfg)

	st := &SessionState{SessionID: sampleSessionID, ExecutionID: "exec_mock_123", PendingSpans: map[string]SpanStart{}}
	if err := SaveState(cfg, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	const filePath = "/Users/dev/work/demo-service/internal/auth/handler.go"
	const contents = "func Secret() string { return \\\"hunter2\\\" }"
	in := HookInput{
		SessionID:     sampleSessionID,
		HookEventName: "PostToolUse",
		ToolName:      "Edit",
		ToolUseID:     "toolu_edit_1",
		ToolInput:     json.RawMessage(`{"file_path":"` + filePath + `","old_string":"a","new_string":"` + contents + `"}`),
		ToolResponse:  json.RawMessage(`{"success":true}`),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := HandlePostTool(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandlePostTool: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	pathSeen, contentsLeaked := false, false
	for _, r := range got {
		if strings.Contains(r.Body, filePath) {
			pathSeen = true
		}
		if strings.Contains(r.Body, "hunter2") {
			contentsLeaked = true
		}
	}
	if !pathSeen {
		t.Errorf("metadata mode should ship the file path, bodies: %+v", got)
	}
	if contentsLeaked {
		t.Errorf("metadata mode must NOT ship file contents (new_string), bodies: %+v", got)
	}
}

// TestMetadataModePrompt verifies prompt text ships in metadata mode.
func TestMetadataModePrompt(t *testing.T) {
	srv, reqs, mu := newMockBackend(t)
	cfg := metadataConfig(t, srv.URL)
	client := NewFact0(cfg)

	in := samplePromptInput()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := HandleUserPrompt(ctx, client, cfg, in); err != nil {
		t.Fatalf("HandleUserPrompt: %v", err)
	}

	mu.Lock()
	got := append([]capturedReq(nil), *reqs...)
	mu.Unlock()

	batch := findReqs(got, func(r capturedReq) bool {
		return r.Method == "POST" && r.Path == "/v1/events/batch"
	})
	if len(batch) == 0 {
		t.Fatalf("expected an audit batch, got: %+v", got)
	}
	if !strings.Contains(batch[0].Body, in.Prompt) {
		t.Errorf("metadata mode should ship prompt text, body: %s", batch[0].Body)
	}
}
