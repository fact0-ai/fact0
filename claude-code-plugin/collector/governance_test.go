package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// writeTempPolicy writes a policy JSON to a temp file and returns its path.
func writeTempPolicy(t *testing.T, p Policy) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

// bashInput builds a HookInput for a Bash tool call with the given command.
func bashInput(cmd string) HookInput {
	return HookInput{
		SessionID: "test-session",
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{"command":` + mustJSON(cmd) + `}`),
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// (a) deny rule from a temp policy file blocks a matching Bash command and
// allows a non-matching one.
func TestEvaluateToolCall_PolicyFileDenyAndAllow(t *testing.T) {
	policyPath := writeTempPolicy(t, Policy{Rules: []PolicyRule{
		{Tool: "Bash", Match: `rm\s+-rf`, Action: "deny", Reason: "denied by test policy"},
	}})
	t.Setenv("FACT0_CC_POLICY_FILE", policyPath)
	t.Setenv("FACT0_CC_ENFORCE", "") // not enforcing; only the file rule is active

	cfg := Config{}

	// Matching command -> denied with the rule's reason.
	d := EvaluateToolCall(cfg, bashInput("rm -rf /tmp/x"))
	if d.Allow {
		t.Fatalf("expected Allow=false for matching deny rule, got Allow=true")
	}
	if d.Reason != "denied by test policy" {
		t.Fatalf("expected reason %q, got %q", "denied by test policy", d.Reason)
	}

	// Non-matching command -> allowed.
	d = EvaluateToolCall(cfg, bashInput("echo hello"))
	if !d.Allow {
		t.Fatalf("expected Allow=true for non-matching command, got Allow=false (reason=%q)", d.Reason)
	}
}

// (b) No policy file and FACT0_CC_ENFORCE unset => allow-all (fail-open).
func TestEvaluateToolCall_FailOpenWhenNoPolicy(t *testing.T) {
	t.Setenv("FACT0_CC_POLICY_FILE", "")
	t.Setenv("FACT0_CC_ENFORCE", "")

	cfg := Config{}

	// Even a clearly dangerous command is allowed when governance is inactive.
	d := EvaluateToolCall(cfg, bashInput("rm -rf /"))
	if !d.Allow {
		t.Fatalf("expected fail-open Allow=true with no policy, got Allow=false (reason=%q)", d.Reason)
	}

	// LoadPolicy must report governance inactive in this state.
	if _, active := LoadPolicy(cfg); active {
		t.Fatalf("expected governance inactive with no policy file and no enforce")
	}
}

// ---- binary-level tests (os/exec) ----

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

// buildBinary compiles the collector once and returns the binary path.
func buildBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fact0-cc-bin")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "fact0-cc")
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		out, err := cmd.CombinedOutput()
		if err != nil {
			binErr = err
			t.Logf("build output: %s", out)
		}
	})
	if binErr != nil {
		t.Fatalf("build binary: %v", binErr)
	}
	return binPath
}

// mockBackend returns an httptest server that accepts any request so the binary
// never blocks on network during post-tool handling.
func mockBackend(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"exec_mock"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cleanEnv returns the current environment with all FACT0_ vars stripped, plus
// the supplied key=value extras appended.
func cleanEnv(extra ...string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		if strings.HasPrefix(kv, "FACT0_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

// runCollector execs the built binary with the given event, stdin payload and
// environment extras; returns stdout, stderr and exit code.
func runCollector(t *testing.T, event, stdin string, env ...string) (string, string, int) {
	t.Helper()
	bin := buildBinary(t)
	srv := mockBackend(t)

	stateDir := t.TempDir()
	fullEnv := cleanEnv(append([]string{
		"FACT0_API_KEY=test",
		"FACT0_BASE_URL=" + srv.URL,
		"FACT0_CC_STATE_DIR=" + stateDir,
	}, env...)...)

	cmd := exec.Command(bin, event)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = fullEnv

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run collector: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// (c) pre-tool DENY path: exact PreToolUse deny JSON on stdout, exit 0.
func TestBinary_PreToolDeny(t *testing.T) {
	policyPath := writeTempPolicy(t, Policy{Rules: []PolicyRule{
		{Tool: "Bash", Match: `rm\s+-rf`, Action: "deny", Reason: "denied by test policy"},
	}})

	stdin := `{"session_id":"s1","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/x"}}`
	stdout, stderr, code := runCollector(t, "pre-tool", stdin,
		"FACT0_CC_POLICY_FILE="+policyPath)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr)
	}

	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"denied by test policy"}}` + "\n"
	if stdout != want {
		t.Fatalf("deny stdout mismatch:\n got: %q\nwant: %q", stdout, want)
	}

	// Sanity: the emitted JSON is the documented PreToolUse deny shape.
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("deny stdout is not valid JSON: %v", err)
	}
	if parsed.HookSpecificOutput.HookEventName != "PreToolUse" ||
		parsed.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("unexpected deny payload fields: %+v", parsed)
	}
}

// (d) pre-tool ALLOW path: empty stdout, exit 0.
func TestBinary_PreToolAllow(t *testing.T) {
	policyPath := writeTempPolicy(t, Policy{Rules: []PolicyRule{
		{Tool: "Bash", Match: `rm\s+-rf`, Action: "deny", Reason: "denied by test policy"},
	}})

	// Command does not match the deny rule -> allow -> no stdout.
	stdin := `{"session_id":"s2","tool_name":"Bash","tool_input":{"command":"echo hello"}}`
	stdout, stderr, code := runCollector(t, "pre-tool", stdin,
		"FACT0_CC_POLICY_FILE="+policyPath)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout on allow, got %q", stdout)
	}
}

// (d2) pre-tool ALLOW path with NO policy at all (fail-open): empty stdout, exit 0.
func TestBinary_PreToolAllow_NoPolicy(t *testing.T) {
	stdin := `{"session_id":"s3","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/x"}}`
	stdout, stderr, code := runCollector(t, "pre-tool", stdin)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout (fail-open) with no policy, got %q", stdout)
	}
}

// (e) Regression: a non-pre-tool event never emits a deny payload (empty stdout).
func TestBinary_PostToolNoStdout(t *testing.T) {
	policyPath := writeTempPolicy(t, Policy{Rules: []PolicyRule{
		{Tool: "Bash", Match: `rm\s+-rf`, Action: "deny", Reason: "denied by test policy"},
	}})

	// Even though the command would match a deny rule, post-tool does not run
	// governance and must produce no stdout.
	stdin := `{"session_id":"s4","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/x"},"tool_response":{"ok":true}}`
	stdout, stderr, code := runCollector(t, "post-tool", stdin,
		"FACT0_CC_POLICY_FILE="+policyPath)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout for post-tool event, got %q", stdout)
	}
}
