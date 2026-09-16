package main

// Each hook is a small write-ahead journal. The original hook and its
// transcript snapshot are persisted before any network call. Preparation
// collects the resulting state and exact outgoing operations in one atomic
// file before delivery. Execution creation during preparation uses the original
// persisted hook ID as its idempotency key. A crash at a delivery boundary
// replays the same IDs and payloads, never a freshly mapped event.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

type hookTransaction struct {
	State      *SessionState
	Operations []DeadLetter
	ID         string
	Timestamp  string
	Input      HookInput
	Err        error
}

type hookEnvelope struct {
	ID          string        `json:"id"`
	Event       string        `json:"event"`
	Input       HookInput     `json:"input"`
	ReceivedAt  string        `json:"received_at"`
	CaptureMode string        `json:"capture_mode,omitempty"`
	Prepared    bool          `json:"prepared"`
	State       *SessionState `json:"state,omitempty"`
	Operations  []DeadLetter  `json:"operations,omitempty"`
	Next        int           `json:"next"`
	Attempts    int           `json:"attempts"`
	LastError   string        `json:"last_error,omitempty"`
	Permanent   bool          `json:"permanent,omitempty"`
	ConfigKey   string        `json:"config_key,omitempty"`
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func lockFile(ctx context.Context, path string, wait bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !wait || (err != syscall.EWOULDBLOCK && err != syscall.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func unlockFile(f *os.File) {
	if f != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

func spoolDir(cfg Config) string { return filepath.Join(cfg.StateDir, "spool") }
func spoolLimit(cfg Config) int64 {
	if cfg.SpoolMaxBytes > 0 {
		return cfg.SpoolMaxBytes
	}
	return 256 << 20
}
func requestLimit(cfg Config) int64 {
	if cfg.RequestMaxBytes > 0 && cfg.RequestMaxBytes < 4<<20 {
		return cfg.RequestMaxBytes
	}
	return 4 << 20
}
func configKey(cfg Config) string {
	return Sha256Hex(fmt.Sprintf("%s|%s|%d|%d|%s", cfg.BaseURL, cfg.CaptureMode, requestLimit(cfg), spoolLimit(cfg), cfg.APIKey))
}

// writeEnvelope checks the aggregate queue budget under a separate lock.
// Existing payloads are never evicted to accept a newer hook.
func writeEnvelope(cfg Config, path string, env *hookEnvelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lock, err := lockFile(ctx, filepath.Join(cfg.StateDir, "spool.lock"), true)
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	dir := spoolDir(cfg)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	var size int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Join(dir, e.Name()) == path {
			continue
		}
		if info, err := e.Info(); err == nil {
			size += info.Size()
		}
	}
	if size+int64(len(data)) > spoolLimit(cfg) {
		return fmt.Errorf("spool full: %d bytes would exceed %d; existing records retained", size+int64(len(data)), spoolLimit(cfg))
	}
	f, err := os.CreateTemp(dir, ".hook-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func recordCaptureFailure(cfg Config, message string) {
	if len(message) > 4096 {
		message = truncate(message, 4096) + " (diagnostic truncated; queued payloads are unchanged)"
	}
	logf("CAPTURE INCOMPLETE: %s", message)
	// A bounded health marker makes full/unwritable queues visible in status.
	_ = os.MkdirAll(cfg.StateDir, 0700)
	data, _ := json.Marshal(map[string]string{"at": nowRFC3339(), "status": "incomplete", "reason": message})
	_ = os.WriteFile(filepath.Join(cfg.StateDir, "capture-error.json"), data, 0600)
}

func enqueueHook(cfg Config, event string, in HookInput) (string, error) {
	if in.Timestamp == "" {
		in.Timestamp = nowRFC3339()
	}
	if event == "stop" || event == "subagent-stop" {
		path := in.TranscriptPath
		if event == "subagent-stop" {
			path = in.AgentTranscriptPath
		}
		stats, _ := ReadTurnFromTranscript(path)
		in.CapturedTurn = &stats
		applyFinalAssistant(&in, event)
	}
	env := &hookEnvelope{ID: newSpanID(), Event: event, Input: in, ReceivedAt: nowRFC3339(), CaptureMode: cfg.CaptureMode}
	path := filepath.Join(spoolDir(cfg), fmt.Sprintf("hook-%020d-%s.json", time.Now().UnixNano(), env.ID))
	return path, writeEnvelope(cfg, path, env)
}

func dispatchHook(ctx context.Context, client *fact0.Client, cfg Config, event string, in HookInput) error {
	switch event {
	case "session-start":
		return HandleSessionStart(ctx, client, cfg, in)
	case "prompt":
		return HandleUserPrompt(ctx, client, cfg, in)
	case "pre-tool":
		return HandlePreTool(ctx, client, cfg, in)
	case "post-tool":
		return HandlePostTool(ctx, client, cfg, in)
	case "post-tool-failure":
		return HandlePostToolFailure(ctx, client, cfg, in)
	case "permission":
		return HandlePermission(ctx, client, cfg, in)
	case "notify":
		return HandleNotification(ctx, client, cfg, in)
	case "subagent-stop":
		return HandleSubagentStop(ctx, client, cfg, in)
	case "stop":
		return HandleStop(ctx, client, cfg, in)
	case "session-end":
		return HandleSessionEnd(ctx, client, cfg, in)
	default:
		return fmt.Errorf("unsupported hook %q", event)
	}
}

func prepareHook(ctx context.Context, client *fact0.Client, cfg Config, env *hookEnvelope) error {
	if env.CaptureMode != "" {
		cfg.CaptureMode = env.CaptureMode
	}
	if env.Event == "stop" || env.Event == "subagent-stop" {
		refineCapturedTurn(ctx, &env.Input, env.Event)
	}
	st, err := LoadState(cfg, env.Input.SessionID)
	if err != nil {
		return err
	}
	tx := &hookTransaction{State: st, ID: env.ID, Timestamp: env.Input.Timestamp, Input: env.Input}
	cfg.transaction = tx
	if err := dispatchHook(ctx, client, cfg, env.Event, env.Input); err != nil {
		return err
	}
	if tx.Err != nil {
		return tx.Err
	}
	env.Prepared = true
	env.State = tx.State
	env.Operations = tx.Operations
	return nil
}

// drainHooks serializes all read/modify/write session operations across hook
// worker processes. Capture itself never waits on this lock or on network I/O.
func drainHooks(ctx context.Context, client *fact0.Client, cfg Config) error {
	lock, err := lockFile(ctx, filepath.Join(cfg.StateDir, "drain.lock"), true)
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	entries, err := os.ReadDir(spoolDir(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	blocked := map[string]bool{}
	var retained error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "hook-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(spoolDir(cfg), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var env hookEnvelope
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&env); err != nil {
			recordCaptureFailure(cfg, "unreadable journal retained: "+entry.Name())
			return fmt.Errorf("unreadable journal retained: %s: %w", entry.Name(), err)
		}
		if env.ID == "" || env.Input.SessionID == "" || env.Next < 0 || env.Next > len(env.Operations) {
			recordCaptureFailure(cfg, "invalid journal retained: "+entry.Name())
			return fmt.Errorf("invalid journal retained: %s", entry.Name())
		}
		if blocked[env.Input.SessionID] {
			continue
		}
		if env.Permanent && env.ConfigKey == configKey(cfg) {
			blocked[env.Input.SessionID] = true
			retained = fmt.Errorf("hook %s requires configuration/repair: %s", env.ID, env.LastError)
			continue
		}
		if !env.Prepared {
			if err = prepareHook(ctx, client, cfg, &env); err == nil {
				err = writeEnvelope(cfg, path, &env)
			}
			if err != nil {
				env.LastError = err.Error()
				env.Attempts++
				_ = writeEnvelope(cfg, path, &env)
				recordCaptureFailure(cfg, err.Error())
				return err
			}
		}
		// State is derived from the durable journal. Reapplying it is safe and
		// required after a crash between preparing and writing the state file.
		if env.State != nil {
			if err := SaveState(cfg, env.State); err != nil {
				recordCaptureFailure(cfg, err.Error())
				return err
			}
		}
		for env.Next < len(env.Operations) {
			op := &env.Operations[env.Next]
			err := deliverOperation(ctx, client, cfg, op)
			if err != nil {
				env.Attempts++
				env.LastError = err.Error()
				env.ConfigKey = configKey(cfg)
				env.Permanent = isPermanentDelivery(err)
				_ = writeEnvelope(cfg, path, &env)
				recordCaptureFailure(cfg, err.Error())
				blocked[env.Input.SessionID] = true
				if !env.Permanent {
					return err
				}
				retained = err
				break
			}
			env.Next++
			env.Permanent = false
			env.LastError = ""
			if err := writeEnvelope(cfg, path, &env); err != nil {
				recordCaptureFailure(cfg, err.Error())
				return err
			}
		}
		if env.Next == len(env.Operations) {
			if err := os.Remove(path); err != nil {
				recordCaptureFailure(cfg, err.Error())
				return err
			}
			_ = syncDir(spoolDir(cfg))
			if env.State != nil && env.State.Closed {
				_ = ClearState(cfg, env.Input.SessionID)
			}
		}
	}
	return retained
}

type permanentDeliveryError struct{ err error }

func (e permanentDeliveryError) Error() string { return e.err.Error() }
func isPermanentDelivery(err error) bool {
	var p permanentDeliveryError
	if errors.As(err, &p) {
		return true
	}
	return strings.Contains(err.Error(), ": 400 ") || strings.Contains(err.Error(), ": 413 ") || strings.Contains(err.Error(), ": 422 ")
}

func operationBody(op *DeadLetter) any {
	switch op.Kind {
	case dlKindAudit:
		return map[string]any{"events": []*fact0.AuditEventInput{op.Audit}}
	case dlKindSpans:
		return map[string]any{"spans": op.Spans}
	case dlKindEndExecution:
		body := map[string]any{"status": op.Status}
		if op.EndedAt != "" {
			body["ended_at"] = op.EndedAt
		}
		return body
	}
	return nil
}
func deliverOperation(ctx context.Context, client *fact0.Client, cfg Config, op *DeadLetter) error {
	data, err := json.Marshal(operationBody(op))
	if err != nil {
		return permanentDeliveryError{err}
	}
	if int64(len(data)) > requestLimit(cfg) {
		return permanentDeliveryError{fmt.Errorf("payload too large: %d bytes exceeds request limit %d; full operation retained", len(data), requestLimit(cfg))}
	}
	return deliverDeadLetter(ctx, client, op)
}

// decodeHook reads at most the configured spool capacity plus one byte. An
// over-capacity hook is explicitly reported rather than silently truncated.
func decodeHook(r io.Reader, cfg Config) (HookInput, error) {
	var in HookInput
	raw, err := io.ReadAll(io.LimitReader(r, spoolLimit(cfg)+1))
	if err != nil {
		return in, err
	}
	if int64(len(raw)) > spoolLimit(cfg) {
		return in, fmt.Errorf("hook exceeds spool capacity %d", spoolLimit(cfg))
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	err = dec.Decode(&in)
	if err == nil && in.SessionID == "" {
		err = fmt.Errorf("missing session_id")
	}
	return in, err
}
