package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func cleanEnv(extra ...string) []string {
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "FACT0_") {
			env = append(env, v)
		}
	}
	return append(env, extra...)
}
func buildTestBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fact0-cc")
	cmd := exec.Command("go", "build", "-o", path, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", out, err)
	}
	return path
}

func TestBinaryHookNeverEnforcesOrWaitsForNetwork(t *testing.T) {
	binary := buildTestBinary(t)
	state := t.TempDir()
	// An unreachable address cannot delay hook capture: the worker owns network.
	env := cleanEnv("FACT0_API_KEY=test", "FACT0_BASE_URL=http://127.0.0.1:9", "FACT0_CC_STATE_DIR="+state, "FACT0_CC_ENFORCE=1", "FACT0_CC_REMOTE_POLICY=1")
	cmd := exec.Command(binary, "pre-tool")
	cmd.Env = env
	cmd.Stdin = strings.NewReader(`{"session_id":"nonblocking","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/nonexistent"}}`)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("hook altered Claude output: %q", stdout.String())
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("capture waited for network: %s", elapsed)
	}
	cfg := Config{StateDir: state}
	if len(queuedHooks(t, cfg)) != 1 {
		t.Fatal("offline hook not retained")
	}
	// Wait for the bounded worker to finish before the test removes its spool.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lock, err := lockFile(ctx, filepath.Join(state, "drain.lock"), true)
	if err == nil {
		unlockFile(lock)
	}
	// Malformed hook input remains nonblocking and produces a visible marker.
	cmd = exec.Command(binary, "prompt")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("{invalid")
	if out, err := cmd.Output(); err != nil || len(out) != 0 {
		t.Fatalf("malformed hook: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(state, "capture-error.json")); err != nil {
		t.Fatal("capture failure marker missing")
	}
}

func TestPrepareDownloadsOnlyDuringExplicitSetup(t *testing.T) {
	plugin := t.TempDir()
	if err := os.Mkdir(filepath.Join(plugin, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../bin/fact0-cc")
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(plugin, "bin", "fact0-cc")
	_ = os.WriteFile(scriptPath, script, 0700)
	_ = os.WriteFile(filepath.Join(plugin, "VERSION"), []byte("0.3.0\n"), 0600)
	payload := []byte("#!/bin/sh\nexit 0\n")
	sum := sha256.Sum256(payload)
	asset := fmt.Sprintf("fact0-cc_0.3.0_%s_%s", runtime.GOOS, runtime.GOARCH)
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		} else {
			_, _ = w.Write(payload)
		}
	}))
	defer srv.Close()
	env := cleanEnv("FACT0_CC_STATE_DIR="+filepath.Join(plugin, "state"), "FACT0_CC_RELEASE_BASE="+srv.URL)
	cmd := exec.Command(scriptPath, "session-start")
	cmd.Env = env
	if out, err := cmd.Output(); err != nil || len(out) != 0 {
		t.Fatalf("unprepared hook: %s %v", out, err)
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 0 {
		t.Fatal("hook downloaded binary")
	}
	cmd = exec.Command(scriptPath, "prepare")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "Collector ready:") {
		t.Fatalf("prepare: %s %v", out, err)
	}
	cmd = exec.Command(scriptPath, "session-start")
	cmd.Env = env
	if out, err := cmd.Output(); err != nil || len(out) != 0 {
		t.Fatalf("prepared hook: %s %v", out, err)
	}
	mu.Lock()
	n = calls
	mu.Unlock()
	if n != 2 {
		t.Fatalf("prepare requests=%d, want binary+checksum only", n)
	}
}

func TestConfigDefaultsRawLocalWithoutRemotePolicy(t *testing.T) {
	t.Setenv("FACT0_BASE_URL", "")
	t.Setenv("FACT0_CC_CAPTURE_MODE", "")
	t.Setenv("FACT0_CC_CAPTURE_RAW", "")
	t.Setenv("FACT0_CC_REMOTE_POLICY", "true")
	t.Setenv("FACT0_API_KEY", "test")
	cfg := LoadConfig()
	if cfg.BaseURL != "http://localhost:8000" || cfg.CaptureMode != CaptureRawMode || cfg.RemotePolicy {
		t.Fatalf("unsafe defaults: %+v", cfg)
	}
}
