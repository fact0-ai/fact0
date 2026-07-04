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

	return auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   "claude_code.session.start",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: map[string]interface{}{
			"cwd":             in.Cwd,
			"permission_mode": in.PermissionMode,
			"source":          in.Source,
			"execution_id":    st.ExecutionID,
		},
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

// HandlePostTool completes the pending tool span, ingests it, and logs an audit event.
func HandlePostTool(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
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

	endedAt := nowRFC3339()
	spanPayload := map[string]any{
		"id":           span.SpanID,
		"execution_id": st.ExecutionID,
		"span_type":    "TOOL_CALL",
		"name":         in.ToolName,
		"status":       "COMPLETED",
		"started_at":   span.StartedAt,
		"ended_at":     endedAt,
		// Span metadata values must be STRINGS: the server decodes metadata as
		// map[string]string and rejects the whole batch on nested objects, so
		// the redacted maps are JSON-encoded.
		"metadata": map[string]any{
			"tool_input": jsonString(RedactInput(in.ToolInput, cfg.CaptureMode)),
			"output":     jsonString(SummarizeOutput(in.ToolResponse, cfg.CaptureMode)),
		},
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

	auditErr := auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    AgentActor(),
		Action:   "claude_code.tool." + strings.ToLower(in.ToolName),
		Resource: ResourceFromTool(in.ToolName, in.ToolInput, cfg.CaptureMode),
		Outcome:  "success",
		Metadata: map[string]interface{}{
			"session_id":  in.SessionID,
			"tool_use_id": in.ToolUseID,
		},
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

// HandleStop closes the active turn span: if state has an execution and a
// pending "__turn__" span, it ingests it as a COMPLETED CUSTOM span, then clears
// the turn from local state. Best-effort — errors propagate so main can log.
func HandleStop(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, err := LoadState(cfg, in.SessionID)
	if err != nil {
		return err
	}

	var ingestErr error
	if turn, ok := st.PendingSpans["__turn__"]; ok {
		if execID := ensureExecution(ctx, client, cfg, in, st); execID != "" {
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
	return ingestErr
}

// HandleSessionEnd ends the execution trace, records session.end, and clears state.
func HandleSessionEnd(ctx context.Context, client *fact0.Client, cfg Config, in HookInput) error {
	st, _ := LoadState(cfg, in.SessionID)

	var endErr error
	if st.ExecutionID != "" {
		endErr = endExecution(ctx, client, cfg, st.ExecutionID, "COMPLETED")
	}

	auditErr := auditLog(ctx, client, cfg, fact0.AuditEventInput{
		Actor:    HumanActor(cfg),
		Action:   "claude_code.session.end",
		Resource: fact0.Resource{ID: in.SessionID, Type: "claude_code.session"},
		Outcome:  "success",
		Metadata: map[string]interface{}{
			"reason":       in.Reason,
			"execution_id": st.ExecutionID,
		},
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
