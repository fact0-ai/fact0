package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
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
// Not dead-lettered: callers need the returned id immediately, so a failure is
// surfaced and retried lazily by later hooks via ensureExecution.
func startExecution(ctx context.Context, client *fact0.Client, in HookInput, trigger string, lazy bool) (string, error) {
	md := map[string]string{
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
		Trigger:        trigger,
		IdempotencyKey: in.SessionID,
		Metadata:       md,
	})
	if err != nil {
		return "", err
	}
	return executionIDFromResponse(resp), nil
}

// ensureExecution returns the session's execution id, lazily starting an
// execution when none exists yet (SessionStart never fired — e.g. the plugin
// was installed mid-session — or its start call failed). On failure it returns
// "" and the caller skips span ingestion; the next hook retries. Hooks run as
// concurrent processes, so after a lazy start the state is re-read and a
// concurrently persisted id wins; the losing execution is ended best-effort so
// no dangling RUNNING row is left behind.
func ensureExecution(ctx context.Context, client *fact0.Client, cfg Config, in HookInput, st *SessionState) string {
	if st.ExecutionID != "" {
		return st.ExecutionID
	}

	id, err := startExecution(ctx, client, in, "lazy", true)
	if err != nil || id == "" {
		logf("lazy execution start failed for session %s: %v", in.SessionID, err)
		return ""
	}

	if fresh, err := LoadState(cfg, in.SessionID); err == nil && fresh.ExecutionID != "" {
		_, _ = client.Telemetry.EndExecution(ctx, id, "COMPLETED")
		st.ExecutionID = fresh.ExecutionID
		return st.ExecutionID
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
	execID, err := startExecution(ctx, client, in, in.Source, false)
	if err != nil {
		return err
	}

	st, _ := LoadState(cfg, in.SessionID)
	st.SessionID = in.SessionID
	st.ExecutionID = execID
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
	// recorded in pending state now and ingested as COMPLETED on Stop.
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
		StartedAt: nowRFC3339(),
	}
	if err := SaveState(cfg, st); err != nil {
		return err
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
	st.PendingSpans[pendingKey(in)] = SpanStart{
		SpanID:    newSpanID(),
		Name:      in.ToolName,
		StartedAt: nowRFC3339(),
	}
	return SaveState(cfg, st)
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
	if len(in.ToolResponse) == 0 || json.Unmarshal(in.ToolResponse, &resp) != nil {
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
	if code, ok := resp["exit_code"].(float64); ok && code != 0 {
		return true
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
	span, ok := st.PendingSpans[key]
	if !ok {
		// No matching pre-tool record; synthesize one so we still emit the span.
		span = SpanStart{SpanID: newSpanID(), Name: in.ToolName, StartedAt: nowRFC3339()}
	}
	delete(st.PendingSpans, key)

	execID := ensureExecution(ctx, client, cfg, in, st)

	failed := forceError || toolCallFailed(in)
	spanStatus, outcome := "COMPLETED", "success"
	if failed {
		spanStatus, outcome = "FAILED", "error"
	}

	endedAt := nowRFC3339()
	spanPayload := map[string]any{
		"id":           span.SpanID,
		"execution_id": st.ExecutionID,
		"span_type":    "TOOL_CALL",
		"name":         in.ToolName,
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
		},
	}
	if failed && in.Error != "" && cfg.CaptureMode != CaptureHash {
		spanPayload["metadata"].(map[string]any)["error"] = truncate(in.Error, 500)
	}
	if st.TurnSpanID != "" {
		spanPayload["parent_span_id"] = st.TurnSpanID
	}

	batch := []map[string]any{spanPayload}
	if miSpan, ok := MaybeModelInvocationSpan(in, st.ExecutionID, st.TurnSpanID); ok {
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
		"execution_id": st.ExecutionID,
	}
	if failed && in.Error != "" && cfg.CaptureMode != CaptureHash {
		auditMD["error"] = truncate(in.Error, 500)
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

// RecordPolicySpan best-effort ingests a POLICY_EVALUATION span recording a
// governance decision. It is point-in-time (started_at == ended_at), nests under
// the active turn span when present, and only ships when the session has an
// ExecutionID. Errors are intentionally swallowed (returns nil) so that policy
// telemetry can never block or alter the enforcement decision.
func RecordPolicySpan(ctx context.Context, client *fact0.Client, cfg Config, in HookInput, allow bool, reason string) error {
	defer func() { _ = recover() }()

	st, _ := LoadState(cfg, in.SessionID)
	if st == nil {
		return nil
	}
	if ensureExecution(ctx, client, cfg, in, st) == "" {
		return nil
	}

	decision := "allow"
	if !allow {
		decision = "deny"
	}

	now := nowRFC3339()
	span := map[string]any{
		"id":           newSpanID(),
		"execution_id": st.ExecutionID,
		"span_type":    "POLICY_EVALUATION",
		"name":         "policy:" + in.ToolName,
		"status":       "COMPLETED",
		"started_at":   now,
		"ended_at":     now,
		"metadata": map[string]any{
			"tool":     in.ToolName,
			"decision": decision,
			"reason":   reason,
		},
	}
	if st.TurnSpanID != "" {
		span["parent_span_id"] = st.TurnSpanID
	}

	_ = ingestSpans(ctx, client, cfg, st.ExecutionID, []map[string]any{span})
	return nil
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
		now := nowRFC3339()
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
		now := nowRFC3339()
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

	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    AgentActor(),
		Action:   "claude_code.subagent.stop",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: map[string]interface{}{
			"agent_type": in.AgentType,
			"agent_id":   in.AgentID,
		},
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
				"ended_at":     nowRFC3339(),
				"metadata":     map[string]any{},
			}
			ingestErr = ingestSpans(ctx, client, cfg, execID, []map[string]any{span})
		}
		delete(st.PendingSpans, "__turn__")
	}

	st.TurnSpanID = ""
	if err := SaveState(cfg, st); err != nil {
		return err
	}

	if stats, ok := ReadTurnFromTranscript(in.TranscriptPath); ok {
		if err := emitTurnComplete(ctx, client, cfg, in, execID, turnSpanID, stats); err != nil {
			logf("turn enrichment: %v", err)
		}
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
	}
	switch cfg.CaptureMode {
	case CaptureRawMode:
		md["response"] = stats.ResponseText
	case CaptureMetadata:
		md["response"] = truncate(stats.ResponseText, turnResponsePreviewLen)
	}

	auditErr := auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    AgentActor(),
		Action:   "claude_code.turn.complete",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: md,
	})

	if execID != "" && (stats.InputTokens > 0 || stats.OutputTokens > 0) {
		now := nowRFC3339()
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

// HandleSessionEnd ends the execution trace, records session.end, and clears state.
func HandleSessionEnd(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, _ := LoadState(cfg, in.SessionID)

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
