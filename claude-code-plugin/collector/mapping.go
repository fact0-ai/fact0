package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// nowRFC3339 returns the current UTC time as an RFC3339 (nano) string.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// jsonString marshals v to a compact JSON string for use as a span metadata
// value (the server only accepts string metadata values).
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// newSpanID generates a random span identifier using crypto/rand only.
func newSpanID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to a timestamp-derived id.
		return "span_" + hex.EncodeToString([]byte(nowRFC3339()))
	}
	return "span_" + hex.EncodeToString(b[:])
}

// HumanActor builds the actor representing the operator driving Claude Code.
func HumanActor(cfg Config) fact0.Actor {
	id := cfg.ActorID
	if id == "" {
		if u := os.Getenv("USER"); u != "" {
			id = u
		} else if u := os.Getenv("USERNAME"); u != "" {
			id = u
		} else {
			id = "unknown"
		}
	}
	return fact0.Actor{ID: id, Type: "human", Email: cfg.ActorEmail}
}

// AgentActor builds the actor representing the Claude Code agent itself.
func AgentActor() fact0.Actor {
	return fact0.Actor{ID: "claude-code", Type: "agent"}
}

// systemActor builds the actor representing the platform/system.
func systemActor() fact0.Actor {
	return fact0.Actor{ID: "claude-code", Type: "system"}
}

// executionIDFromResponse extracts the execution id from a StartExecution response,
// preferring "id" and falling back to "execution_id".
func executionIDFromResponse(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if v, ok := resp["id"].(string); ok && v != "" {
		return v
	}
	if v, ok := resp["execution_id"].(string); ok && v != "" {
		return v
	}
	return ""
}

// startExecution begins an execution trace for a session and returns its id.
// Failed starts leave the original hook journal pending; retries use its key.
func startExecution(ctx context.Context, client *fact0.Client, in HookInput, trigger, key string, lazy bool) (string, error) {
	md := map[string]string{
		"session_id":      in.SessionID,
		"cwd":             in.Cwd,
		"permission_mode": in.PermissionMode,
		"source":          in.Source,
	}
	if lazy {
		md["lazy_start"] = "true"
	}
	if gi := ReadGitInfo(ctx, in.Cwd); gi.Branch != "" || gi.Commit != "" {
		md["git_branch"] = gi.Branch
		md["git_commit"] = gi.Commit
		md["git_remote"] = gi.Remote
	}
	resp, err := client.Telemetry.StartExecution(ctx, fact0.StartExecutionRequest{
		AgentID:        "claude-code",
		AgentName:      "Claude Code",
		StartedAt:      in.Timestamp,
		Trigger:        trigger,
		IdempotencyKey: key,
		Metadata:       md,
	})
	if err != nil {
		return "", err
	}
	return executionIDFromResponse(resp), nil
}

// executionKey survives a crash before StartExecution responds: in journal
// mode it derives from the already persisted hook ID, never a fresh retry ID.
func executionKey(cfg Config, st *SessionState) string {
	if st.ExecutionKey == "" {
		if cfg.transaction != nil {
			st.ExecutionKey = "cc-" + cfg.transaction.ID
		} else {
			st.ExecutionKey = "cc-" + newSpanID()
		}
	}
	return st.ExecutionKey
}

// ensureExecution returns the session's execution id, lazily starting an
// execution when none exists yet. The journal keeps failed starts for retry;
// the worker lock serializes state changes across hook processes.
func ensureExecution(ctx context.Context, client *fact0.Client, cfg Config, in HookInput, st *SessionState) string {
	if st.Closed {
		*st = SessionState{SessionID: in.SessionID, PendingSpans: map[string]SpanStart{}}
	}
	if st.ExecutionID != "" {
		return st.ExecutionID
	}

	key := executionKey(cfg, st)
	id, err := startExecution(ctx, client, in, "lazy", key, true)
	if err != nil || id == "" {
		logf("lazy execution start failed for session %s: %v", in.SessionID, err)
		if cfg.transaction != nil {
			cfg.transaction.Err = fmt.Errorf("execution start unavailable: %v", err)
		}
		return ""
	}

	st.ExecutionID = id
	if err := SaveState(cfg, st); err != nil {
		logf("persisting lazily started execution %s: %v", id, err)
	}
	logf("lazily started execution %s for session %s", id, in.SessionID)
	return id
}

// HandleSessionStart begins a new execution trace and records a session.start event.
func HandleSessionStart(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, _ := LoadState(cfg, in.SessionID)
	if st.Closed {
		*st = SessionState{SessionID: in.SessionID, PendingSpans: map[string]SpanStart{}}
	}
	execID := st.ExecutionID
	if execID == "" {
		var err error
		execID, err = startExecution(ctx, client, in, in.Source, executionKey(cfg, st), false)
		if err != nil {
			return err
		}
		if execID == "" {
			return fmt.Errorf("backend omitted execution id")
		}
	}
	st.SessionID = in.SessionID
	st.ExecutionID = execID
	st.Closed = false
	if st.PendingSpans == nil {
		st.PendingSpans = map[string]SpanStart{}
	}
	if err := SaveState(cfg, st); err != nil {
		return err
	}

	md := map[string]interface{}{
		"cwd":             in.Cwd,
		"permission_mode": in.PermissionMode,
		"source":          in.Source,
		"execution_id":    st.ExecutionID,
	}
	if gi := ReadGitInfo(ctx, in.Cwd); gi.Branch != "" || gi.Commit != "" {
		md["git_branch"] = gi.Branch
		md["git_commit"] = gi.Commit
		md["git_remote"] = gi.Remote
	}
	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   "claude_code.session.start",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: md,
	})
}

// HandleUserPrompt records the submission of a user prompt as an audit event.
func HandleUserPrompt(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	md := map[string]interface{}{
		"cwd":           in.Cwd,
		"prompt_sha256": Sha256Hex(in.Prompt),
		"prompt_len":    len(in.Prompt),
	}
	// The prompt is the developer's own words — it ships in metadata and raw
	// modes so sessions are readable; hash mode keeps only the digest.
	if cfg.CaptureMode != CaptureHash {
		md["prompt"] = in.Prompt
	}

	// Open a turn span: tool spans for this prompt nest under it. The span is
	// ingested as RUNNING now, before any children, and completed on Stop.
	st, _ := LoadState(cfg, in.SessionID)
	if st.SessionID == "" {
		st.SessionID = in.SessionID
	}
	if st.PendingSpans == nil {
		st.PendingSpans = map[string]SpanStart{}
	}
	_ = ensureExecution(ctx, client, cfg, in, st)
	turnSpanID := newSpanID()
	st.TurnSpanID = turnSpanID
	st.PendingSpans["__turn__"] = SpanStart{
		SpanID:    turnSpanID,
		Name:      "turn",
		StartedAt: hookTime(cfg),
		Metadata:  map[string]string{"session_id": in.SessionID, "capture_mode": cfg.CaptureMode},
	}
	if cfg.CaptureMode != CaptureHash {
		turn := st.PendingSpans["__turn__"]
		turn.Metadata["prompt"] = in.Prompt
		st.PendingSpans["__turn__"] = turn
	}
	if err := SaveState(cfg, st); err != nil {
		return err
	}
	if st.ExecutionID != "" {
		turn := st.PendingSpans["__turn__"]
		md := map[string]any{"session_id": in.SessionID, "capture_mode": cfg.CaptureMode}
		if cfg.CaptureMode != CaptureHash {
			md["prompt"] = in.Prompt
		}
		if err := ingestSpans(ctx, client, cfg, st.ExecutionID, []map[string]any{{"id": turn.SpanID, "execution_id": st.ExecutionID, "span_type": "CUSTOM", "name": "turn", "status": "RUNNING", "started_at": turn.StartedAt, "metadata": md}}); err != nil {
			return err
		}
	}

	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   "claude_code.prompt.submit",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: md,
	})
}

// pendingKey selects the correlation key for a tool span: prefer ToolUseID, else ToolName.
func pendingKey(in HookInput) string {
	if in.ToolUseID != "" {
		return in.ToolUseID
	}
	return in.ToolName
}

// HandlePreTool records the start of a tool call into pending span state.
func HandlePreTool(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, err := LoadState(cfg, in.SessionID)
	if err != nil {
		return err
	}
	if st.SessionID == "" {
		st.SessionID = in.SessionID
	}
	if st.PendingSpans == nil {
		st.PendingSpans = map[string]SpanStart{}
	}
	if in.ToolUseID != "" && st.CompletedTools[pendingKey(in)] {
		return nil
	}
	if _, exists := st.PendingSpans[pendingKey(in)]; exists {
		return nil
	}
	st.PendingSpans[pendingKey(in)] = SpanStart{
		SpanID:    newSpanID(),
		Name:      in.ToolName,
		StartedAt: hookTime(cfg),
		Input:     append(json.RawMessage(nil), in.ToolInput...), ParentSpanID: st.TurnSpanID,
	}
	if err := SaveState(cfg, st); err != nil {
		return err
	}
	if cfg.transaction != nil {
		if ensureExecution(ctx, client, cfg, in, st) == "" {
			return nil
		}
		start := st.PendingSpans[pendingKey(in)]
		metadata := map[string]string{"tool_input": jsonString(RedactInput(in.ToolInput, cfg.CaptureMode)), "tool_use_id": in.ToolUseID, "session_id": in.SessionID, "capture_mode": cfg.CaptureMode}
		start.Metadata = metadata
		st.PendingSpans[pendingKey(in)] = start
		span := map[string]any{"id": start.SpanID, "execution_id": st.ExecutionID, "span_type": "TOOL_CALL", "name": start.Name, "status": "RUNNING", "started_at": start.StartedAt, "metadata": metadata}
		if st.TurnSpanID != "" {
			span["parent_span_id"] = st.TurnSpanID
		}
		return ingestSpans(ctx, client, cfg, st.ExecutionID, []map[string]any{span})
	}
	return nil
}

// HandlePostTool completes the pending tool span, ingests it, and logs an
// audit event. The outcome is sniffed from the payload (error markers, Bash
// exit codes) so failed tool calls stop masquerading as successes.
func HandlePostTool(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	return handlePostTool(ctx, client, cfg, in, false)
}

// HandlePostToolFailure handles the PostToolUseFailure hook: same shape as
// post-tool but the outcome is unconditionally an error.
func HandlePostToolFailure(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	return handlePostTool(ctx, client, cfg, in, true)
}

// toolCallFailed reports whether the tool call failed, from the explicit
// error field, common error markers in tool_response, or a non-zero Bash
// exit code. Mirrors the server-side agentless mapping so both capture paths
// agree on outcomes.
func toolCallFailed(in HookInput) bool {
	if in.Error != "" {
		return true
	}
	var resp map[string]any
	dec := json.NewDecoder(bytes.NewReader(in.ToolResponse))
	dec.UseNumber()
	if len(in.ToolResponse) == 0 || dec.Decode(&resp) != nil {
		return false
	}
	if b, ok := resp["is_error"].(bool); ok && b {
		return true
	}
	if b, ok := resp["success"].(bool); ok && !b {
		return true
	}
	if s, ok := resp["status"].(string); ok && strings.EqualFold(s, "error") {
		return true
	}
	if s, ok := resp["error"].(string); ok && s != "" {
		return true
	}
	if code, ok := resp["exit_code"].(json.Number); ok {
		if n, err := code.Float64(); err == nil && n != 0 {
			return true
		}
	}
	return false
}

func handlePostTool(ctx context.Context, client *fact0.Client, cfg Config, in HookInput, forceError bool) error {
	st, err := LoadState(cfg, in.SessionID)
	if err != nil {
		return err
	}
	if st.PendingSpans == nil {
		st.PendingSpans = map[string]SpanStart{}
	}

	key := pendingKey(in)
	if in.ToolUseID != "" && st.CompletedTools[key] {
		return nil
	}
	span, ok := st.PendingSpans[key]
	if !ok {
		// No matching pre-tool record; synthesize one so we still emit the span.
		span = SpanStart{SpanID: newSpanID(), Name: in.ToolName, StartedAt: hookTime(cfg), ParentSpanID: st.TurnSpanID}
	}
	if in.ToolName == "" {
		in.ToolName = span.Name
	}
	if len(in.ToolInput) == 0 {
		in.ToolInput = span.Input
	}
	delete(st.PendingSpans, key)
	if st.CompletedTools == nil {
		st.CompletedTools = map[string]bool{}
	}
	if in.ToolUseID != "" {
		st.CompletedTools[key] = true
	}

	execID := ensureExecution(ctx, client, cfg, in, st)

	failed := forceError || toolCallFailed(in)
	spanStatus, outcome := "COMPLETED", "success"
	if failed {
		spanStatus, outcome = "FAILED", "error"
	}

	endedAt := hookTime(cfg)
	spanPayload := map[string]any{
		"id":           span.SpanID,
		"execution_id": st.ExecutionID,
		"span_type":    "TOOL_CALL",
		"name":         span.Name,
		"status":       spanStatus,
		"started_at":   span.StartedAt,
		"ended_at":     endedAt,
		// Span metadata values must be STRINGS: the server decodes metadata as
		// map[string]string and rejects the whole batch on nested objects, so
		// the redacted maps are JSON-encoded.
		"metadata": map[string]any{
			"tool_input": jsonString(RedactInput(in.ToolInput, cfg.CaptureMode)),
			"output":     jsonString(SummarizeOutput(in.ToolResponse, cfg.CaptureMode)),
			// Lets the dashboard join this span to its audit tool event for
			// per-tool durations.
			"tool_use_id": in.ToolUseID,
			"session_id":  in.SessionID, "capture_mode": cfg.CaptureMode,
		},
	}
	if failed && in.Error != "" && cfg.CaptureMode != CaptureHash {
		spanPayload["metadata"].(map[string]any)["error"] = capturedError(in.Error, cfg)
	}
	if span.ParentSpanID != "" {
		spanPayload["parent_span_id"] = span.ParentSpanID
	}

	batch := []map[string]any{spanPayload}
	if miSpan, ok := MaybeModelInvocationSpan(in, st.ExecutionID, span.ParentSpanID); ok {
		batch = append(batch, miSpan)
	}

	var ingestErr error
	if execID != "" {
		ingestErr = ingestSpans(ctx, client, cfg, execID, batch)
	}

	auditMD := map[string]interface{}{
		"session_id":  in.SessionID,
		"tool_use_id": in.ToolUseID,
		// Links lazily-started executions into the session rollup: the
		// backend aggregation reads execution_id off event metadata.
		"execution_id":  st.ExecutionID,
		"tool_input":    RedactInput(in.ToolInput, cfg.CaptureMode),
		"tool_response": SummarizeOutput(in.ToolResponse, cfg.CaptureMode),
	}
	if failed && in.Error != "" && cfg.CaptureMode != CaptureHash {
		auditMD["error"] = capturedError(in.Error, cfg)
	}
	auditErr := auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    AgentActor(),
		Action:   "claude_code.tool." + strings.ToLower(in.ToolName),
		Resource: ResourceFromTool(in.ToolName, in.ToolInput, cfg.CaptureMode),
		Outcome:  outcome,
		Metadata: auditMD,
	})

	if err := SaveState(cfg, st); err != nil {
		return err
	}
	if ingestErr != nil {
		return ingestErr
	}
	return auditErr
}

// HandlePermission records a human permission decision (PermissionRequest /
// PermissionDenied hooks, newer Claude Code builds) as an audit event plus a
// point-in-time HUMAN_APPROVAL span — accountability for dangerous actions
// lands on a person, not the agent.
func HandlePermission(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	denied := strings.EqualFold(in.HookEventName, "PermissionDenied")
	action, decision, outcome := "claude_code.permission.request", "requested", "success"
	if denied {
		action, decision, outcome = "claude_code.permission.denied", "denied", "failure"
	}

	st, _ := LoadState(cfg, in.SessionID)
	if execID := ensureExecution(ctx, client, cfg, in, st); execID != "" {
		now := hookTime(cfg)
		span := map[string]any{
			"id":           newSpanID(),
			"execution_id": execID,
			"span_type":    "HUMAN_APPROVAL",
			"name":         "permission:" + in.ToolName,
			"status":       "COMPLETED",
			"started_at":   now,
			"ended_at":     now,
			"metadata": map[string]any{
				"tool":     in.ToolName,
				"decision": decision,
			},
		}
		if st.TurnSpanID != "" {
			span["parent_span_id"] = st.TurnSpanID
		}
		_ = ingestSpans(ctx, client, cfg, execID, []map[string]any{span})
	}

	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   action,
		Resource: ResourceFromTool(in.ToolName, in.ToolInput, cfg.CaptureMode),
		Outcome:  outcome,
		Metadata: map[string]interface{}{
			"session_id":   in.SessionID,
			"tool_name":    in.ToolName,
			"decision":     decision,
			"execution_id": st.ExecutionID,
		},
	})
}

// HandleNotification records a Claude Code notification as a system audit event.
func HandleNotification(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    systemActor(),
		Action:   "claude_code.notification",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: map[string]interface{}{
			"message": in.Message,
		},
	})
}

// HandleSubagentStop records the termination of a subagent. It ingests a
// point-in-time CUSTOM span nested under the active turn span and logs an audit
// event.
func HandleSubagentStop(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, _ := LoadState(cfg, in.SessionID)

	if execID := ensureExecution(ctx, client, cfg, in, st); execID != "" {
		now := hookTime(cfg)
		span := map[string]any{
			"id":           newSpanID(),
			"execution_id": execID,
			"span_type":    "CUSTOM",
			"name":         "subagent:" + in.AgentType,
			"status":       "COMPLETED",
			"started_at":   now,
			"ended_at":     now,
			"metadata": map[string]any{
				"agent_type": in.AgentType,
				"agent_id":   in.AgentID,
			},
		}
		if st.TurnSpanID != "" {
			span["parent_span_id"] = st.TurnSpanID
		}
		if err := ingestSpans(ctx, client, cfg, execID, []map[string]any{span}); err != nil {
			return err
		}
	}

	stats := in.CapturedTurn
	if stats == nil {
		s, _ := ReadTurnFromTranscript(in.AgentTranscriptPath)
		stats = &s
	}
	metadata := map[string]interface{}{"agent_type": in.AgentType, "agent_id": in.AgentID, "capture_status": stats.CaptureStatus, "capture_reason": stats.CaptureReason, "response_len": len(stats.ResponseText), "response_sha256": Sha256Hex(stats.ResponseText)}
	if cfg.RawCapture() {
		metadata["response"] = stats.ResponseText
		metadata["messages"] = stats.Messages
	} else if cfg.CaptureMode == CaptureMetadata {
		metadata["response"] = truncate(stats.ResponseText, turnResponsePreviewLen)
		metadata["response_truncated"] = len(stats.ResponseText) > turnResponsePreviewLen
	}

	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    AgentActor(),
		Action:   "claude_code.subagent.stop",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: metadata,
	})
}

// HandleStop closes the active turn span and enriches the turn from the
// transcript: hook payloads never carry the assistant's response or token
// usage, but the transcript JSONL does. Emits a claude_code.turn.complete
// audit event (response text gated by capture mode) and a MODEL_INVOCATION
// span so token rollups work. Transcript work is strictly best-effort.
func HandleStop(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, err := LoadState(cfg, in.SessionID)
	if err != nil {
		return err
	}

	execID := ensureExecution(ctx, client, cfg, in, st)
	turnSpanID := ""

	var ingestErr error
	if turn, ok := st.PendingSpans["__turn__"]; ok {
		turnSpanID = turn.SpanID
		if execID != "" {
			span := map[string]any{
				"id":           turn.SpanID,
				"execution_id": execID,
				"span_type":    "CUSTOM",
				"name":         "turn",
				"status":       "COMPLETED",
				"started_at":   turn.StartedAt,
				"ended_at":     hookTime(cfg),
				"metadata":     turn.Metadata,
			}
			ingestErr = ingestSpans(ctx, client, cfg, execID, []map[string]any{span})
		}
		delete(st.PendingSpans, "__turn__")
	}

	st.TurnSpanID = ""
	if err := SaveState(cfg, st); err != nil {
		return err
	}

	stats := in.CapturedTurn
	if stats == nil {
		s, _ := ReadTurnFromTranscript(in.TranscriptPath)
		stats = &s
	}
	if err := emitTurnComplete(ctx, client, cfg, in, execID, turnSpanID, *stats); err != nil {
		return err
	}
	return ingestErr
}

// turnResponsePreviewLen bounds the assistant-response text shipped in
// metadata mode; raw mode ships the full text, hash mode only the digest.
const turnResponsePreviewLen = 1500

// emitTurnComplete records the assistant's side of a turn: an audit event
// carrying the response (capture-mode gated) and a MODEL_INVOCATION span with
// real token usage from the transcript.
func emitTurnComplete(ctx context.Context, client *fact0.Client, cfg Config, in HookInput, execID, turnSpanID string, stats TurnStats) error {
	md := map[string]interface{}{
		"session_id":      in.SessionID,
		"execution_id":    execID,
		"model":           stats.Model,
		"input_tokens":    stats.InputTokens,
		"output_tokens":   stats.OutputTokens,
		"response_len":    len(stats.ResponseText),
		"response_sha256": Sha256Hex(stats.ResponseText),
		"capture_status":  stats.CaptureStatus,
		"capture_reason":  stats.CaptureReason,
	}
	switch cfg.CaptureMode {
	case CaptureRawMode:
		md["response"] = stats.ResponseText
		md["messages"] = stats.Messages
	case CaptureMetadata:
		md["response"] = truncate(stats.ResponseText, turnResponsePreviewLen)
		md["response_truncated"] = len(stats.ResponseText) > turnResponsePreviewLen
	}

	auditErr := auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    AgentActor(),
		Action:   "claude_code.turn.complete",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: md,
	})

	if execID != "" && (stats.InputTokens > 0 || stats.OutputTokens > 0) {
		now := hookTime(cfg)
		model := stats.Model
		if model == "" {
			model = "claude"
		}
		span := map[string]any{
			"id":           newSpanID(),
			"execution_id": execID,
			"span_type":    "MODEL_INVOCATION",
			"name":         "model_invocation",
			"status":       "COMPLETED",
			"started_at":   now,
			"ended_at":     now,
			"metadata":     map[string]any{},
			"model_invocation": map[string]any{
				"model_name":        model,
				"model_provider":    "anthropic",
				"prompt_tokens":     stats.InputTokens,
				"completion_tokens": stats.OutputTokens,
				"total_tokens":      stats.InputTokens + stats.OutputTokens,
			},
		}
		if turnSpanID != "" {
			span["parent_span_id"] = turnSpanID
		}
		if err := ingestSpans(ctx, client, cfg, execID, []map[string]any{span}); err != nil {
			return err
		}
	}
	return auditErr
}

func capturedError(s string, cfg Config) string {
	if cfg.RawCapture() {
		return s
	}
	return truncate(s, 500)
}

// HandleSessionEnd ends the execution trace, records session.end, and clears state.
func HandleSessionEnd(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, _ := LoadState(cfg, in.SessionID)

	// Closing a session without matching Stop/Post hooks leaves visible partial
	// captures, not silently RUNNING children under a finished execution.
	if st.ExecutionID != "" && len(st.PendingSpans) > 0 {
		keys := make([]string, 0, len(st.PendingSpans))
		for key := range st.PendingSpans {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var cancelled []map[string]any
		for _, key := range keys {
			pending := st.PendingSpans[key]
			kind := "TOOL_CALL"
			if key == "__turn__" {
				kind = "CUSTOM"
			}
			metadata := map[string]string{}
			for k, v := range pending.Metadata {
				metadata[k] = v
			}
			metadata["capture_status"] = "partial"
			metadata["capture_reason"] = "session ended before completion hook"
			span := map[string]any{"id": pending.SpanID, "execution_id": st.ExecutionID, "span_type": kind, "name": pending.Name, "status": "CANCELLED", "started_at": pending.StartedAt, "ended_at": hookTime(cfg), "metadata": metadata}
			if pending.ParentSpanID != "" {
				span["parent_span_id"] = pending.ParentSpanID
			}
			cancelled = append(cancelled, span)
		}
		if err := ingestSpans(ctx, client, cfg, st.ExecutionID, cancelled); err != nil {
			return err
		}
	}

	var endErr error
	if st.ExecutionID != "" {
		endErr = endExecution(ctx, client, cfg, st.ExecutionID, "COMPLETED")
	}

	endMD := map[string]interface{}{
		"reason":       in.Reason,
		"execution_id": st.ExecutionID,
	}
	// End-of-session HEAD: with the start commit, this brackets what the
	// session produced ("abc123 -> def456").
	if gi := ReadGitInfo(ctx, in.Cwd); gi.Commit != "" {
		endMD["git_commit_end"] = gi.Commit
		endMD["git_branch"] = gi.Branch
	}
	auditErr := auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   "claude_code.session.end",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: endMD,
	})

	clearErr := ClearState(cfg, in.SessionID)

	if endErr != nil {
		return endErr
	}
	if auditErr != nil {
		return auditErr
	}
	return clearErr
}

func hookTime(cfg Config) string {
	if cfg.transaction != nil && cfg.transaction.Timestamp != "" {
		return cfg.transaction.Timestamp
	}
	return nowRFC3339()
}
