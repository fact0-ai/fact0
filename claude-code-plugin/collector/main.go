package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
func run() (result int) {
	defer func() {
		if v := recover(); v != nil {
			logf("capture panic recovered: %v", v)
			result = 0
		}
	}()
	cfg := LoadConfig()
	if len(os.Args) < 2 {
		logf("missing event name argument")
		return 0
	}
	event := os.Args[1]
	if event == "status" {
		printStatus(cfg)
		return 0
	}
	if event == "verify" {
		printVerify(cfg)
		return 0
	}
	if cfg.Disabled {
		return 0
	}
	if cfg.APIKey == "" {
		recordCaptureFailure(cfg, "FACT0_API_KEY is not set; hook was not captured")
		return 0
	}
	if event == "flush" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		drainHooks(ctx, NewFact0(cfg), cfg)
		return 0
	}
	in, err := decodeHook(os.Stdin, cfg)
	if err != nil {
		recordCaptureFailure(cfg, "parse hook: "+err.Error())
		return 0
	}
	if _, err := enqueueHook(cfg, event, in); err != nil {
		recordCaptureFailure(cfg, err.Error())
		return 0
	}
	// Only the durable local append is synchronous. Delivery runs in an
	// independent process with no inherited hook pipes, so Claude can proceed.
	if err := startWorker(); err != nil {
		recordCaptureFailure(cfg, "hook retained; could not start delivery worker: "+err.Error())
	}
	return 0
}

func startWorker() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "flush")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
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
	switch v := resp["valid"].(type) {
	case bool:
		verified = v
	case string:
		verified = isTruthy(v)
	}

	fmt.Println("fact0-collector verify")
	fmt.Printf("  valid: %v\n", verified)
	if c, ok := resp["events_checked"]; ok {
		fmt.Printf("  events_checked: %v\n", c)
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

	fmt.Println("  governance:  disabled in this release")
	entriesPending, _ := os.ReadDir(spoolDir(cfg))
	pending := 0
	permanent := 0
	for _, e := range entriesPending {
		if strings.HasPrefix(e.Name(), "hook-") && strings.HasSuffix(e.Name(), ".json") {
			pending++
			data, _ := os.ReadFile(filepath.Join(spoolDir(cfg), e.Name()))
			var env hookEnvelope
			if json.Unmarshal(data, &env) == nil && env.Permanent {
				permanent++
			}
		}
	}
	fmt.Printf("  spool:       %d hooks pending (%d require configuration/repair), limit %d bytes\n", pending, permanent, spoolLimit(cfg))
	if data, err := os.ReadFile(filepath.Join(cfg.StateDir, "capture-error.json")); err == nil {
		fmt.Printf("  last_capture_error: %s\n", data)
	}

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
