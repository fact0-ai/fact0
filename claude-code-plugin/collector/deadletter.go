package main

import (
	"bytes"
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
	EndedAt     string           `json:"ended_at,omitempty"`
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
// letter whose payload is unusable is retained with an explicit error.
func deliverDeadLetter(ctx context.Context, client *fact0.Client, dl *DeadLetter) error {
	switch dl.Kind {
	case dlKindAudit:
		if dl.Audit == nil {
			return permanentDeliveryError{fmt.Errorf("missing audit payload")}
		}
		resp, err := client.Audit.LogBatch(ctx, []fact0.AuditEventInput{*dl.Audit})
		if err != nil {
			return err
		}
		return checkAcceptance(resp, "accepted", 1)
	case dlKindSpans:
		if dl.ExecutionID == "" || len(dl.Spans) == 0 {
			return permanentDeliveryError{fmt.Errorf("missing span payload or execution id")}
		}
		resp, err := client.Telemetry.IngestSpans(ctx, dl.ExecutionID, dl.Spans)
		if err != nil {
			return err
		}
		return checkAcceptance(resp, "accepted_count", len(dl.Spans))
	case dlKindEndExecution:
		if dl.ExecutionID == "" {
			return permanentDeliveryError{fmt.Errorf("missing execution id")}
		}
		_, err := client.Telemetry.EndExecutionAt(ctx, dl.ExecutionID, dl.Status, dl.EndedAt)
		return err
	default:
		return permanentDeliveryError{fmt.Errorf("unknown operation kind %q", dl.Kind)}
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
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&dl); err != nil {
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
	if event.ID == "" {
		event.ID = "evt_" + strings.TrimPrefix(newSpanID(), "span_")
	}
	if event.Timestamp == "" {
		if cfg.transaction != nil {
			event.Timestamp = cfg.transaction.Timestamp
		} else {
			event.Timestamp = nowRFC3339()
		}
	}
	if event.Metadata == nil {
		event.Metadata = map[string]interface{}{}
	}
	event.Metadata["capture_mode"] = cfg.CaptureMode
	if cfg.transaction != nil {
		event.Metadata["session_id"] = cfg.transaction.Input.SessionID
		if cfg.transaction.State != nil {
			event.Metadata["execution_id"] = cfg.transaction.State.ExecutionID
		}
		cfg.transaction.Operations = append(cfg.transaction.Operations, DeadLetter{Kind: dlKindAudit, Audit: &event})
		return nil
	}
	err := deliverOperation(ctx, client, cfg, &DeadLetter{Kind: dlKindAudit, Audit: &event})
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
	if cfg.transaction != nil {
		// Split only at span boundaries. A single oversize item is preserved
		// whole and becomes an explicit permanent delivery error, never clipped.
		var batch []map[string]any
		flush := func() {
			if len(batch) > 0 {
				cfg.transaction.Operations = append(cfg.transaction.Operations, DeadLetter{Kind: dlKindSpans, ExecutionID: executionID, Spans: batch})
				batch = nil
			}
		}
		for _, span := range spans {
			candidate := append(append([]map[string]any{}, batch...), span)
			raw, _ := json.Marshal(map[string]any{"spans": candidate})
			if len(batch) > 0 && int64(len(raw)) > requestLimit(cfg) {
				flush()
			}
			batch = append(batch, span)
		}
		flush()
		return nil
	}
	err := deliverOperation(ctx, client, cfg, &DeadLetter{Kind: dlKindSpans, ExecutionID: executionID, Spans: spans})
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
	endedAt := hookTime(cfg)
	if cfg.transaction != nil {
		cfg.transaction.Operations = append(cfg.transaction.Operations, DeadLetter{Kind: dlKindEndExecution, ExecutionID: executionID, Status: status, EndedAt: endedAt})
		return nil
	}
	_, err := client.Telemetry.EndExecutionAt(ctx, executionID, status, endedAt)
	if err == nil {
		return nil
	}
	if dlErr := SaveDeadLetter(cfg, &DeadLetter{Kind: dlKindEndExecution, ExecutionID: executionID, Status: status, EndedAt: endedAt}); dlErr != nil {
		logf("end-execution failed (%v); dead-letter save also failed: %v", err, dlErr)
		return err
	}
	logf("end-execution failed, queued to dead-letter: %v", err)
	return nil
}

func checkAcceptance(resp map[string]any, countField string, want int) error {
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		return permanentDeliveryError{fmt.Errorf("batch item rejection: %s", jsonString(errs))}
	}
	if rejected, ok := resp["rejected"]; ok {
		if n, valid := usageNumber(rejected); valid && n > 0 {
			return permanentDeliveryError{fmt.Errorf("audit batch rejected %v items", rejected)}
		}
	}
	var n int
	switch v := resp[countField].(type) {
	case float64:
		n = int(v)
	case json.Number:
		i, _ := v.Int64()
		n = int(i)
	case int:
		n = v
	default:
		return fmt.Errorf("backend response missing %s; acceptance unconfirmed", countField)
	}
	if n != want {
		return permanentDeliveryError{fmt.Errorf("backend accepted %d of %d items", n, want)}
	}
	return nil
}
