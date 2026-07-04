package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// Dead-letter kinds, one per backend operation the collector can replay.
const (
	dlKindAudit        = "audit"
	dlKindSpans        = "spans"
	dlKindEndExecution = "end_execution"
)

const (
	// maxReplayPerRun bounds how many dead letters a single hook invocation
	// attempts to redeliver, so replay can never noticeably delay a session.
	maxReplayPerRun = 25
	// maxDeliveryAttempts is the number of redeliveries before a dead letter
	// is abandoned (renamed *.abandoned and never retried again). This stops
	// permanently-rejected payloads from being retried forever.
	maxDeliveryAttempts = 20

	dlPrefix    = "dl-"
	dlSuffix    = ".json"
	dlAbandoned = ".abandoned"
)

// DeadLetter is one undelivered backend payload persisted to disk so a later
// hook invocation can redeliver it. Exactly one of the kind-specific field
// sets is populated, selected by Kind.
type DeadLetter struct {
	Kind     string `json:"kind"`
	SavedAt  string `json:"saved_at"`
	Attempts int    `json:"attempts"`

	// Kind == dlKindAudit
	Audit *fact0.AuditEventInput `json:"audit,omitempty"`

	// Kind == dlKindSpans / dlKindEndExecution
	ExecutionID string           `json:"execution_id,omitempty"`
	Spans       []map[string]any `json:"spans,omitempty"`
	Status      string           `json:"status,omitempty"`
}

// deadLetterDir returns the directory holding undelivered payloads.
func deadLetterDir(cfg Config) string {
	return filepath.Join(cfg.StateDir, "deadletter")
}

// deadLetterFileName builds a lexically-sortable unique file name so replay
// preserves rough delivery order.
func deadLetterFileName() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s%020d-%s%s", dlPrefix, time.Now().UTC().UnixNano(), hex.EncodeToString(b[:]), dlSuffix)
}

// writeDeadLetterFile atomically persists a dead letter to path (temp file +
// rename, 0600, directory 0700), mirroring SaveState's durability contract.
func writeDeadLetterFile(dir, path string, dl *DeadLetter) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(dl)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dl-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
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
	return os.Rename(tmpName, path)
}

// SaveDeadLetter persists an undelivered payload for later replay.
func SaveDeadLetter(cfg Config, dl *DeadLetter) error {
	if dl.SavedAt == "" {
		dl.SavedAt = nowRFC3339()
	}
	dir := deadLetterDir(cfg)
	return writeDeadLetterFile(dir, filepath.Join(dir, deadLetterFileName()), dl)
}

// CountDeadLetters reports how many undelivered payloads are pending replay.
func CountDeadLetters(cfg Config) int {
	entries, err := os.ReadDir(deadLetterDir(cfg))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if isDeadLetterFile(e) {
			n++
		}
	}
	return n
}

func isDeadLetterFile(e os.DirEntry) bool {
	name := e.Name()
	return !e.IsDir() && strings.HasPrefix(name, dlPrefix) && strings.HasSuffix(name, dlSuffix)
}

// deliverDeadLetter attempts to send one dead letter to the backend. A dead
// letter whose payload is unusable (e.g. missing execution id) is treated as
// delivered so it gets removed rather than retried forever.
func deliverDeadLetter(ctx context.Context, client *fact0.Client, dl *DeadLetter) error {
	switch dl.Kind {
	case dlKindAudit:
		if dl.Audit == nil {
			return nil
		}
		return client.Audit.Log(ctx, *dl.Audit)
	case dlKindSpans:
		if dl.ExecutionID == "" || len(dl.Spans) == 0 {
			return nil
		}
		_, err := client.Telemetry.IngestSpans(ctx, dl.ExecutionID, dl.Spans)
		return err
	case dlKindEndExecution:
		if dl.ExecutionID == "" {
			return nil
		}
		_, err := client.Telemetry.EndExecution(ctx, dl.ExecutionID, dl.Status)
		return err
	default:
		logf("dropping dead letter with unknown kind %q", dl.Kind)
		return nil
	}
}

// ReplayDeadLetters redelivers pending dead letters, oldest first, capped at
// maxReplayPerRun. It stops at the first delivery failure (the backend is
// likely still unreachable) after bumping that letter's attempt count.
// Corrupt files and letters that exhaust maxDeliveryAttempts are renamed with
// an .abandoned suffix so they stay inspectable but are never retried.
// Best-effort throughout: it never returns an error and never writes stdout.
func ReplayDeadLetters(ctx context.Context, client *fact0.Client, cfg Config) {
	dir := deadLetterDir(cfg)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no dead-letter dir: nothing pending
	}
	var names []string
	for _, e := range entries {
		if isDeadLetterFile(e) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	replayed := 0
	for _, name := range names {
		if replayed >= maxReplayPerRun {
			logf("dead-letter replay cap reached (%d); %d still pending", maxReplayPerRun, len(names)-replayed)
			return
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var dl DeadLetter
		if err := json.Unmarshal(data, &dl); err != nil {
			logf("abandoning corrupt dead letter %s: %v", name, err)
			_ = os.Rename(path, path+dlAbandoned)
			continue
		}

		if err := deliverDeadLetter(ctx, client, &dl); err != nil {
			dl.Attempts++
			if dl.Attempts >= maxDeliveryAttempts {
				logf("abandoning dead letter %s after %d attempts: %v", name, dl.Attempts, err)
				_ = os.Rename(path, path+dlAbandoned)
				return
			}
			if werr := writeDeadLetterFile(dir, path, &dl); werr != nil {
				logf("dead-letter attempt update failed for %s: %v", name, werr)
			}
			logf("dead-letter replay stopped at %s (attempt %d): %v", name, dl.Attempts, err)
			return
		}
		_ = os.Remove(path)
		replayed++
	}
	if replayed > 0 {
		logf("redelivered %d dead letter(s)", replayed)
	}
}

// auditLog delivers one audit event, dead-lettering it on failure. The event
// timestamp is stamped up front so a later replay preserves the original
// event time rather than the redelivery time. Returns nil when the event was
// either delivered or durably queued; an error means the event was lost.
func auditLog(ctx context.Context, client *fact0.Client, cfg Config, event fact0.AuditEventInput) error {
	if event.Timestamp == "" {
		event.Timestamp = nowRFC3339()
	}
	err := client.Audit.Log(ctx, event)
	if err == nil {
		return nil
	}
	if dlErr := SaveDeadLetter(cfg, &DeadLetter{Kind: dlKindAudit, Audit: &event}); dlErr != nil {
		logf("audit send failed (%v); dead-letter save also failed: %v", err, dlErr)
		return err
	}
	logf("audit send failed, queued to dead-letter: %v", err)
	return nil
}

// ingestSpans delivers a span batch, dead-lettering it on failure. Same
// delivered-or-queued contract as auditLog.
func ingestSpans(ctx context.Context, client *fact0.Client, cfg Config, executionID string, spans []map[string]any) error {
	_, err := client.Telemetry.IngestSpans(ctx, executionID, spans)
	if err == nil {
		return nil
	}
	if dlErr := SaveDeadLetter(cfg, &DeadLetter{Kind: dlKindSpans, ExecutionID: executionID, Spans: spans}); dlErr != nil {
		logf("span ingest failed (%v); dead-letter save also failed: %v", err, dlErr)
		return err
	}
	logf("span ingest failed, queued to dead-letter: %v", err)
	return nil
}

// endExecution ends an execution, dead-lettering the call on failure. Same
// delivered-or-queued contract as auditLog.
func endExecution(ctx context.Context, client *fact0.Client, cfg Config, executionID, status string) error {
	_, err := client.Telemetry.EndExecution(ctx, executionID, status)
	if err == nil {
		return nil
	}
	if dlErr := SaveDeadLetter(cfg, &DeadLetter{Kind: dlKindEndExecution, ExecutionID: executionID, Status: status}); dlErr != nil {
		logf("end-execution failed (%v); dead-letter save also failed: %v", err, dlErr)
		return err
	}
	logf("end-execution failed, queued to dead-letter: %v", err)
	return nil
}
