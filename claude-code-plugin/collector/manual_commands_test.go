package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

func commandExit(t *testing.T, program, command string, environment []string, want int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, command)
	cmd.Env = environment
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want {
		t.Fatalf("%s %s wanted exit %d: %v (%s)", program, command, want, err, out)
	}
	return string(out)
}

func TestManualCommandsReportFailures(t *testing.T) {
	binary := buildTestBinary(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/events/batch":
			_, _ = w.Write([]byte(`{"accepted":0,"rejected":1,"errors":[{"index":0,"code":"INVALID_INPUT","message":"fixture rejected"}]}`))
		case r.Header.Get("Authorization") == "Bearer invalid-chain":
			_, _ = w.Write([]byte(`{"valid":false,"events_checked":3}`))
		case r.Header.Get("Authorization") == "Bearer denied":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"fixture denied"}`))
		default:
			_, _ = w.Write([]byte(`{"valid":true,"events_checked":3}`))
		}
	}))
	defer srv.Close()
	state := t.TempDir()
	env := func(key string) []string {
		return cleanEnv("FACT0_CC_STATE_DIR="+state, "FACT0_BASE_URL="+srv.URL, "FACT0_API_KEY="+key)
	}
	commandExit(t, binary, "verify", env(""), 1)
	if out := commandExit(t, binary, "verify", env("invalid-chain"), 1); !strings.Contains(out, "valid: false") {
		t.Fatalf("invalid-chain diagnostic missing: %s", out)
	}
	commandExit(t, binary, "verify", env("denied"), 1)
	if out := commandExit(t, binary, "verify", env("valid"), 0); !strings.Contains(out, "events_checked: 3") {
		t.Fatalf("verification result missing: %s", out)
	}
	commandExit(t, binary, "flush", env(""), 1)
	commandExit(t, binary, "flush", env("valid"), 0)
	commandExit(t, binary, "status", env(""), 0)
	commandExit(t, binary, "flush", append(env("valid"), "FACT0_CC_DISABLED=1"), 1)

	// A real HTTP 200 item rejection must fail manual flush on both the first
	// attempt and a later attempt that retains the permanent journal entry.
	cfg := Config{StateDir: state}
	journal := hookEnvelope{
		ID: "manual-rejection", Event: "notify", Input: HookInput{SessionID: "manual-rejection"}, Prepared: true,
		Operations: []DeadLetter{{Kind: dlKindAudit, Audit: &fact0.AuditEventInput{Action: "fixture"}}},
	}
	path := filepath.Join(spoolDir(cfg), "hook-manual-rejection.json")
	if err := writeEnvelope(cfg, path, &journal); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		commandExit(t, binary, "flush", env("valid"), 1)
		if saved := readEnvelope(t, path); !saved.Permanent || !strings.Contains(saved.LastError, "batch item rejection") {
			t.Fatalf("rejected operation not retained: %+v", saved)
		}
	}
	// Reporting a pending permanent item is a successful status operation.
	commandExit(t, binary, "status", env("valid"), 0)

	unreadable := filepath.Join(t.TempDir(), "state-is-a-file")
	if err := os.WriteFile(unreadable, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	badStateEnv := cleanEnv("FACT0_CC_STATE_DIR="+unreadable, "FACT0_BASE_URL="+srv.URL, "FACT0_API_KEY=valid")
	commandExit(t, binary, "status", badStateEnv, 1)
	commandExit(t, binary, "flush", badStateEnv, 1)
}

func TestWrapperPreservesOnlyManualExitCodes(t *testing.T) {
	plugin := t.TempDir()
	bin := filepath.Join(plugin, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../bin/fact0-cc")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(bin, "fact0-cc")
	if err := os.WriteFile(wrapper, script, 0700); err != nil {
		t.Fatal(err)
	}
	collector := filepath.Join(bin, "fixture-collector")
	if err := os.WriteFile(collector, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := cleanEnv("FACT0_CC_BIN="+collector, "FACT0_CC_STATE_DIR="+filepath.Join(plugin, "state"))
	for _, command := range []string{"verify", "flush", "status"} {
		commandExit(t, wrapper, command, env, 7)
	}
	for _, command := range []string{"session-start", "prompt", "pre-tool", "post-tool", "post-tool-failure", "permission", "notify", "subagent-stop", "stop", "session-end"} {
		commandExit(t, wrapper, command, env, 0)
	}
	commandExit(t, wrapper, "prepare", env, 0)
	// Missing preparation also has to be distinguishable by manual callers.
	env = cleanEnv("FACT0_CC_STATE_DIR=" + filepath.Join(plugin, "state"))
	for _, command := range []string{"verify", "flush", "status"} {
		commandExit(t, wrapper, command, env, 1)
	}
	commandExit(t, wrapper, "session-start", env, 0)
}
