package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// logf writes a diagnostic line to stderr. The collector must NEVER write to
// stdout: anything on stdout is interpreted by Claude Code and can alter the
// session.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fact0-collector: "+format+"\n", args...)
}

// main is the entry point for the Claude Code hook collector. It is invoked as
// a short-lived subprocess once per hook event:
//
//	collector <event-name>   (event payload as JSON on stdin)
//
// It must NEVER exit non-zero and NEVER print to stdout, so that a collector
// failure can never block or perturb the user's Claude Code session.
func main() {
	os.Exit(run())
}

// run performs the work and always returns 0 in the MVP. It exists so that
// deferred cleanup runs before the process exits via main's os.Exit.
func run() int {
	cfg := LoadConfig()

	// "status" is a manual (non-hook) subcommand: it is run by a human, so it
	// MAY print to stdout. It requires no network, works even when the
	// collector is unconfigured/disabled, and always exits 0.
	if len(os.Args) >= 2 && os.Args[1] == "status" {
		printStatus(cfg)
		return 0
	}

	// "verify" is a manual (non-hook) subcommand. It MAY print to stdout. It
	// walks the audit hash chain via the backend and prints a concise summary,
	// always exiting 0.
	if len(os.Args) >= 2 && os.Args[1] == "verify" {
		printVerify(cfg)
		return 0
	}

	if cfg.Disabled || cfg.APIKey == "" {
		// Silently no-op: collector is off or unconfigured.
		return 0
	}

	if len(os.Args) < 2 {
		logf("missing event name argument")
		return 0
	}
	event := os.Args[1]

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		logf("read stdin: %v", err)
		return 0
	}

	var in HookInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			logf("parse hook input for %q: %v", event, err)
			return 0
		}
	}

	client := NewFact0(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Redeliver any events that failed to send on earlier invocations. Skipped
	// on pre-tool, which is the only synchronous hook (governance) and must
	// stay fast.
	if event != "pre-tool" {
		ReplayDeadLetters(ctx, client, cfg)
	}

	var handlerErr error
	switch event {
	case "session-start":
		handlerErr = HandleSessionStart(ctx, client, cfg, in)
		// Force-refresh the server-managed policy at the session boundary
		// (after telemetry so the 8s budget isn't starved).
		RefreshRemotePolicy(ctx, client, cfg, 0)
	case "prompt":
		handlerErr = HandleUserPrompt(ctx, client, cfg, in)
	case "pre-tool":
		// Governance runs first. The entire block is fail-open: any panic or
		// error must result in ALLOW (no stdout, exit 0) so a policy bug can
		// never brick the user's session.
		if denied := runGovernance(ctx, client, cfg, in); denied {
			// EmitDenyDecision has already written the deny payload to stdout.
			return 0
		}
		handlerErr = HandlePreTool(ctx, client, cfg, in)
	case "post-tool":
		handlerErr = HandlePostTool(ctx, client, cfg, in)
	case "post-tool-failure":
		handlerErr = HandlePostToolFailure(ctx, client, cfg, in)
	case "permission":
		handlerErr = HandlePermission(ctx, client, cfg, in)
	case "notify":
		handlerErr = HandleNotification(ctx, client, cfg, in)
	case "subagent-stop":
		handlerErr = HandleSubagentStop(ctx, client, cfg, in)
	case "stop":
		handlerErr = HandleStop(ctx, client, cfg, in)
		// Opportunistic policy refresh at turn boundaries when stale.
		RefreshRemotePolicy(ctx, client, cfg, 5*time.Minute)
	case "session-end":
		handlerErr = HandleSessionEnd(ctx, client, cfg, in)
	default:
		logf("unknown event %q", event)
		return 0
	}

	if handlerErr != nil {
		logf("handler %q error: %v", event, handlerErr)
	}

	// MVP contract: always exit 0, never block the session.
	return 0
}

// runGovernance evaluates the pre-tool call against the active policy and, on a
// deny, emits the deny payload to stdout and returns true. It is fully
// fail-open: any panic or error degrades to ALLOW (returns false, no stdout).
// A best-effort POLICY_EVALUATION span is recorded regardless of the outcome.
func runGovernance(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) (denied bool) {
	defer func() {
		if r := recover(); r != nil {
			logf("governance block panic recovered (fail-open allow): %v", r)
			denied = false
		}
	}()

	d := EvaluateToolCall(cfg, in)

	// Telemetry is best-effort and must never affect the decision.
	_ = RecordPolicySpan(ctx, client, cfg, in, d.Allow, d.Reason)

	if !d.Allow {
		EmitDenyDecision(d.Reason)
		return true
	}
	return false
}

// printVerify walks the audit hash chain via the backend and prints a concise
// summary to stdout. It always exits 0 and is only invoked from the manual
// "verify" subcommand.
func printVerify(cfg Config) {
	if cfg.APIKey == "" {
		fmt.Println("fact0-collector verify: FACT0_API_KEY not set; nothing to verify.")
		return
	}

	client := NewFact0(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Audit.Verify(ctx, "")
	if err != nil {
		fmt.Printf("fact0-collector verify: could not reach backend: %v\n", err)
		return
	}

	verified := false
	switch v := resp["verified"].(type) {
	case bool:
		verified = v
	case string:
		verified = isTruthy(v)
	}

	fmt.Println("fact0-collector verify")
	fmt.Printf("  verified: %v\n", verified)
	if c, ok := resp["count"]; ok {
		fmt.Printf("  count:    %v\n", c)
	}
	if msg, ok := resp["message"].(string); ok && msg != "" {
		fmt.Printf("  message:  %s\n", msg)
	}
}

// maskKey returns a privacy-preserving rendering of an API key for display.
func maskKey(key string) string {
	if key == "" {
		return "<not set>"
	}
	if len(key) <= 4 {
		return "set (****)"
	}
	return "set (****" + key[len(key)-4:] + ")"
}

// onOff renders a boolean as a human-friendly on/off string.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// printStatus writes a concise, human-readable collector status to stdout. It
// is only ever invoked from the manual "status" subcommand (never a hook), so
// writing to stdout is safe here. It performs no network calls.
func printStatus(cfg Config) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "<default>"
	}
	actorID := cfg.ActorID
	if actorID == "" {
		actorID = "<default: OS user>"
	}

	fmt.Println("fact0-collector status")
	fmt.Printf("  api_key:     %s\n", maskKey(cfg.APIKey))
	fmt.Printf("  base_url:    %s\n", baseURL)
	fmt.Printf("  actor_id:    %s\n", actorID)
	fmt.Printf("  capture_mode: %s\n", cfg.CaptureMode)
	fmt.Printf("  disabled:    %s\n", onOff(cfg.Disabled))
	fmt.Printf("  state_dir:   %s\n", cfg.StateDir)
	fmt.Printf("  dead_letter: %d pending\n", CountDeadLetters(cfg))

	// Governance / policy summary.
	policyFile := strings.TrimSpace(os.Getenv("FACT0_CC_POLICY_FILE"))
	if policyFile == "" {
		policyFile = "<not set>"
	}
	pol, active := LoadPolicy(cfg)
	fmt.Printf("  policy_file: %s\n", policyFile)
	fmt.Printf("  enforce:     %s\n", onOff(isTruthy(os.Getenv("FACT0_CC_ENFORCE"))))
	fmt.Printf("  remote_policy: %s\n", onOff(cfg.RemotePolicy))
	if cache, ok := LoadPolicyCache(cfg); ok {
		fmt.Printf("  policy_cache:  v%d · %d rule(s) · fetched %s\n",
			cache.Version, len(cache.Rules), cache.FetchedAt.Format(time.RFC3339))
	}
	fmt.Printf("  governance:  %s (%d rule(s))\n", onOff(active), len(pol.Rules))

	fmt.Println("  active sessions:")
	entries, err := os.ReadDir(cfg.StateDir)
	if err != nil {
		fmt.Printf("    <none: %v>\n", err)
		return
	}
	found := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(cfg.StateDir, name))
		if rerr != nil {
			continue
		}
		var st SessionState
		if json.Unmarshal(data, &st) != nil {
			continue
		}
		sid := st.SessionID
		if sid == "" {
			sid = "<unknown>"
		}
		eid := st.ExecutionID
		if eid == "" {
			eid = "<none>"
		}
		fmt.Printf("    %s -> %s\n", sid, eid)
		found++
	}
	if found == 0 {
		fmt.Println("    <none>")
	}
}
