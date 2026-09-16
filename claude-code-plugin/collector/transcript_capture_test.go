package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDelayedFinalTranscriptUsesCapturedTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	prefix := `{"type":"user","uuid":"prompt1","message":{"content":"first turn"}}
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"Working"}]}}
`
	final := `{"type":"assistant","uuid":"a2","message":{"id":"m2","content":[{"type":"text","text":"Final 東京🙂"}],"usage":{"output_tokens":42}}}
{"type":"user","uuid":"prompt2","message":{"content":"later turn"}}
{"type":"assistant","uuid":"a3","message":{"id":"m3","content":[{"type":"text","text":"Do not mix this turn"}]}}
`
	if err := os.WriteFile(path, []byte(prefix), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{StateDir: filepath.Join(dir, "state"), CaptureMode: CaptureRawMode}
	queue, err := enqueueHook(cfg, "stop", HookInput{SessionID: "delayed", TranscriptPath: path, LastAssistantMessage: stringPointer("Final 東京🙂")})
	if err != nil {
		t.Fatal(err)
	}
	env := readEnvelope(t, queue)
	if env.Input.CapturedTurn.CaptureStatus != "partial" || !strings.Contains(env.Input.CapturedTurn.ResponseText, "Final 東京🙂") {
		t.Fatal("hook final text was not preserved before transcript flush")
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(120 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			_, err = f.WriteString(final)
			_ = f.Close()
		}
		done <- err
	}()
	refineCapturedTurn(context.Background(), &env.Input, "stop")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stats := env.Input.CapturedTurn
	if stats.CaptureStatus != "complete" || stats.ResponseText != "Working\nFinal 東京🙂" || len(stats.Messages) != 2 || stats.OutputTokens != 42 {
		t.Fatalf("delayed transcript not recovered exactly: %+v", stats)
	}
}

func TestMissingFinalTranscriptKeepsHookTextPartial(t *testing.T) {
	in := HookInput{SessionID: "missing", LastAssistantMessage: stringPointer("Complete hook final text café🙂")}
	applyFinalAssistant(&in, "stop")
	refineCapturedTurn(context.Background(), &in, "stop")
	if in.CapturedTurn.ResponseText != *in.LastAssistantMessage || in.CapturedTurn.CaptureStatus != "partial" || len(in.CapturedTurn.Messages) != 1 {
		t.Fatalf("hook fallback=%+v", in.CapturedTurn)
	}
}

func TestOlderStopWithoutFinalMarkerIsExplicitlyPartial(t *testing.T) {
	stats, _ := ReadTurnFromTranscript("testdata/transcript-turn.jsonl")
	in := HookInput{CapturedTurn: &stats}
	applyFinalAssistant(&in, "stop")
	if stats.CaptureStatus != "partial" || !strings.Contains(stats.CaptureReason, "cannot be confirmed") {
		t.Fatalf("unconfirmed transcript mislabeled: %+v", stats)
	}
}
