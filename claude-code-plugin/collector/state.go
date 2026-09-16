package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SpanStart records an in-flight span so that PostToolUse can correlate with
// the matching PreToolUse event.
type SpanStart struct {
	SpanID       string            `json:"span_id"`
	Name         string            `json:"name"`
	StartedAt    string            `json:"started_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
}

// SessionState is the per-session state persisted between hook invocations.
// Each Claude Code hook runs as its own process, so state is durable on disk.
type SessionState struct {
	SessionID      string               `json:"session_id"`
	ExecutionKey   string               `json:"execution_key,omitempty"`
	ExecutionID    string               `json:"execution_id"`
	TurnSpanID     string               `json:"turn_span_id,omitempty"`
	PendingSpans   map[string]SpanStart `json:"pending_spans,omitempty"` // key = ToolUseID or tool name
	CompletedTools map[string]bool      `json:"completed_tools,omitempty"`
	Closed         bool                 `json:"closed,omitempty"`
}

// safeSessionID sanitizes a session id for use as a filename component,
// guarding against path traversal and odd characters.
func safeSessionID(sessionID string) string {
	if sessionID == "" {
		return "default"
	}
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, sessionID)
	// Avoid hidden/relative names like "." or ".." after mapping.
	if mapped == "" || mapped == "." || mapped == ".." {
		return "default-" + Sha256Hex(sessionID)[:16]
	}
	if mapped != sessionID {
		mapped += "-" + Sha256Hex(sessionID)[:16]
	}
	return mapped
}

// StatePath returns the absolute path to the state file for a session.
func StatePath(cfg Config, sessionID string) string {
	return filepath.Join(cfg.StateDir, "session-"+safeSessionID(sessionID)+".json")
}

// LoadState reads the persisted state for a session. If the file does not
// exist, a zero-value (non-nil) state with the SessionID populated is returned
// and no error. Corrupt files are retained and reported.
func LoadState(cfg Config, sessionID string) (*SessionState, error) {
	if cfg.transaction != nil {
		return cfg.transaction.State, nil
	}
	st := &SessionState{
		SessionID:    sessionID,
		PendingSpans: map[string]SpanStart{},
	}
	path := StatePath(cfg, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	if len(data) == 0 {
		return st, errors.New("empty session state")
	}
	if err := json.Unmarshal(data, st); err != nil {
		return st, err
	}
	if st.SessionID == "" {
		st.SessionID = sessionID
	}
	if st.PendingSpans == nil {
		st.PendingSpans = map[string]SpanStart{}
	}
	return st, nil
}

// SaveState atomically persists session state. The state directory is created
// with 0700 and the file written with 0600 via a temp file + rename.
func SaveState(cfg Config, st *SessionState) error {
	if st == nil {
		return errors.New("nil session state")
	}
	if cfg.transaction != nil {
		cfg.transaction.State = st
		return nil
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(cfg.StateDir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	path := StatePath(cfg, st.SessionID)
	tmp, err := os.CreateTemp(cfg.StateDir, ".session-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(cfg.StateDir)
}

// ClearState removes the persisted state file for a session. A missing file is
// not an error.
func ClearState(cfg Config, sessionID string) error {
	if cfg.transaction != nil {
		cfg.transaction.State.Closed = true
		return nil
	}
	err := os.Remove(StatePath(cfg, sessionID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
