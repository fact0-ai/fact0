package rest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
)

// ─── Claude Code agentless ingest ──────────────────────────
//
// POST /v1/integrations/claude-code accepts native Claude Code hook payloads
// (the JSON each hook writes to stdin) and maps them onto the same audit-event
// hash chain that powers /v1/events/batch. It exists so an operator can stream
// Claude Code activity straight from their hook config without running the
// collector binary; the mapping here mirrors the collector's
// (claude-code-plugin/collector: mapping.go + resource.go) one-for-one so both
// paths produce identical audit rows.
//
// Persistence, auth, rate-limiting, monthly-limit checks and the response shape
// are all delegated to the existing AuditHandler machinery — this file only
// adds the hook→AuditEvent translation layer.

// claudeHookEvent is a single native Claude Code hook payload. Field presence
// varies by hook event; absent fields stay at their zero value. `event` is
// accepted as an alias for `hook_event_name` for forward compatibility.
type claudeHookEvent struct {
	HookEventName  string          `json:"hook_event_name"`
	Event          string          `json:"event"`
	SessionID      string          `json:"session_id"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	Prompt         string          `json:"prompt"`
	Source         string          `json:"source"`
	Cwd            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	AgentType      string          `json:"agent_type"`
	Message        string          `json:"message"`
	Reason         string          `json:"reason"`
	Error          string          `json:"error"`
	Timestamp      *time.Time      `json:"timestamp"`
}

// claudeIngestRequest is the flexible request envelope: callers may POST either
// a single hook event (the bare object) OR {"events":[...]} for a batch. The
// Events field is decoded first; when absent the whole body is treated as one
// event.
type claudeIngestRequest struct {
	Events []claudeHookEvent `json:"events"`
}

// MountIntegrations installs agentless integration ingest routes. Mount under
// the same chain as MountWrite (auth.APIKey + rate limiter) — write scope is
// enforced internally by RequireWrite, mirroring MountWrite exactly.
func (h *AuditHandler) MountIntegrations(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireWrite)
		r.Post("/v1/integrations/claude-code", h.IngestClaudeCode)
	})
}

// IngestClaudeCode decodes native Claude Code hook payloads, maps them onto
// audit events and persists them through the shared batch pipeline. The tenant
// and identity come from the auth.APIKey middleware (same as /v1/events/batch).
func (h *AuditHandler) IngestClaudeCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBatchBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hooks, err := decodeClaudeHooks(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(hooks) == 0 {
		writeError(w, http.StatusBadRequest, "no events in request")
		return
	}

	captureRaw := claudeCaptureRaw(r)

	events := make([]*audit.AuditEvent, len(hooks))
	for i := range hooks {
		events[i] = hookToAuditEvent(hooks[i], captureRaw)
	}

	tenantID := auth.TenantFromContext(r.Context())

	if err := h.checkMonthlyLimit(r.Context(), tenantID, len(events)); err != nil {
		writeAppError(w, err)
		return
	}

	if h.useAsync(r) {
		h.enqueueBatch(w, r, tenantID, events)
		return
	}

	res, err := h.svc.LogBatch(r.Context(), tenantID, events)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("claude-code ingest failed")
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// decodeClaudeHooks accepts either {"events":[...]} or a single bare hook
// object and normalises both into a slice.
func decodeClaudeHooks(body []byte) ([]claudeHookEvent, error) {
	// Probe for the batch envelope first without committing to a shape.
	var probe struct {
		Events json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, err
	}
	if len(probe.Events) > 0 {
		var req claudeIngestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		return req.Events, nil
	}

	var single claudeHookEvent
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, err
	}
	return []claudeHookEvent{single}, nil
}

// claudeCaptureRaw reports whether the caller opted in to shipping raw
// prompt/command text. Default is false: only SHA-256 digests are stored.
func claudeCaptureRaw(r *http.Request) bool {
	if r.URL.Query().Get("capture_raw") == "true" {
		return true
	}
	return r.Header.Get("X-Fact0-Capture-Raw") == "true"
}

// hookToAuditEvent maps one Claude Code hook payload onto an AuditEvent,
// mirroring the collector's mapping. ID/Timestamp/PrevHash/Hash/SequenceNumber
// are intentionally left for the service + repo to fill.
func hookToAuditEvent(ev claudeHookEvent, captureRaw bool) *audit.AuditEvent {
	name := ev.HookEventName
	if name == "" {
		name = ev.Event
	}

	sessionResource := audit.Resource{
		ID:   nonEmpty(ev.SessionID, "unknown"),
		Type: "claude_code.session",
	}

	out := &audit.AuditEvent{
		Outcome:  audit.OutcomeSuccess,
		Metadata: map[string]interface{}{},
	}
	if ev.Timestamp != nil {
		out.Timestamp = *ev.Timestamp
	}
	if ev.SessionID != "" {
		out.Metadata["session_id"] = ev.SessionID
	}

	switch name {
	case "SessionStart":
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorHuman}
		out.Action = "claude_code.session.start"
		out.Resource = sessionResource
		putNonEmpty(out.Metadata, "cwd", ev.Cwd)
		putNonEmpty(out.Metadata, "permission_mode", ev.PermissionMode)
		putNonEmpty(out.Metadata, "source", ev.Source)

	case "SessionEnd":
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorHuman}
		out.Action = "claude_code.session.end"
		out.Resource = sessionResource
		putNonEmpty(out.Metadata, "reason", ev.Reason)

	case "UserPromptSubmit":
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorHuman}
		out.Action = "claude_code.prompt.submit"
		out.Resource = sessionResource
		out.Metadata["prompt_sha256"] = sha256Hex(ev.Prompt)
		out.Metadata["prompt_len"] = len(ev.Prompt)
		putNonEmpty(out.Metadata, "cwd", ev.Cwd)
		if captureRaw {
			out.Metadata["prompt"] = ev.Prompt
		}

	case "Notification":
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorSystem}
		out.Action = "claude_code.notification"
		out.Resource = sessionResource
		putNonEmpty(out.Metadata, "message", ev.Message)

	case "SubagentStop":
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorAgent}
		out.Action = "claude_code.subagent.stop"
		out.Resource = sessionResource
		putNonEmpty(out.Metadata, "agent_type", ev.AgentType)

	case "Stop":
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorAgent}
		out.Action = "claude_code.stop"
		out.Resource = sessionResource

	case "PreToolUse", "PostToolUse":
		tool := nonEmpty(ev.ToolName, "unknown")
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorAgent}
		out.Action = "claude_code.tool." + strings.ToLower(tool)
		out.Resource = resourceFromTool(tool, ev.ToolInput, captureRaw)
		out.Metadata["tool_name"] = tool
		applyToolInputMetadata(out.Metadata, tool, ev.ToolInput, captureRaw)
		if hookHasError(name, ev) {
			out.Outcome = audit.OutcomeError
		}

	default:
		out.Actor = audit.Actor{ID: "claude-code", Type: audit.ActorSystem}
		out.Action = "claude_code." + normalizeEventName(name)
		out.Resource = sessionResource
		if ev.ToolName != "" {
			out.Metadata["tool_name"] = ev.ToolName
		}
		if hookHasError(name, ev) {
			out.Outcome = audit.OutcomeError
		}
	}

	return out
}

// resourceFromTool mirrors collector/resource.go: it derives the resource a
// tool call targets, hashing raw text unless capture_raw is set.
func resourceFromTool(toolName string, toolInput json.RawMessage, captureRaw bool) audit.Resource {
	var fields map[string]any
	if len(toolInput) > 0 {
		_ = json.Unmarshal(toolInput, &fields)
	}

	switch {
	case toolName == "Bash":
		cmd := stringFieldOf(fields, "command")
		name := ""
		if captureRaw {
			name = truncateStr(cmd, 80)
		}
		return audit.Resource{ID: sha256Hex(cmd), Type: "shell.command", Name: name}

	case toolName == "Edit" || toolName == "Write" || toolName == "MultiEdit" || toolName == "NotebookEdit":
		path := stringFieldOf(fields, "file_path")
		if path == "" {
			path = stringFieldOf(fields, "notebook_path")
		}
		if !captureRaw {
			return audit.Resource{ID: sha256Hex(path), Type: "file"}
		}
		return audit.Resource{ID: path, Type: "file", Name: filepath.Base(path)}

	case strings.HasPrefix(toolName, "mcp__"):
		return audit.Resource{ID: toolName, Type: "mcp.tool", Name: toolName}

	default:
		return audit.Resource{ID: toolName, Type: "claude_code.tool", Name: toolName}
	}
}

// applyToolInputMetadata records the (hashed by default) tool input. Bash
// commands get a dedicated command_sha256/command_len pair; everything else is
// recursively redacted.
func applyToolInputMetadata(md map[string]interface{}, toolName string, toolInput json.RawMessage, captureRaw bool) {
	if len(toolInput) == 0 {
		return
	}
	if toolName == "Bash" {
		var fields map[string]any
		_ = json.Unmarshal(toolInput, &fields)
		cmd := stringFieldOf(fields, "command")
		md["command_sha256"] = sha256Hex(cmd)
		md["command_len"] = len(cmd)
		if captureRaw {
			md["command"] = cmd
		}
		return
	}
	md["tool_input"] = redactRawJSON(toolInput, captureRaw)
}

// hookHasError reports whether a tool hook represents a failed call. It honours
// an explicit *Failure event name, a top-level error field, or common error
// markers inside tool_response.
func hookHasError(name string, ev claudeHookEvent) bool {
	if strings.Contains(name, "Failure") {
		return true
	}
	if ev.Error != "" {
		return true
	}
	return responseLooksError(ev.ToolResponse)
}

// responseLooksError inspects a tool_response object for common error markers.
func responseLooksError(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	if v, ok := m["error"]; ok && v != nil {
		if s, isStr := v.(string); !isStr || s != "" {
			return true
		}
	}
	if v, ok := m["is_error"].(bool); ok && v {
		return true
	}
	if v, ok := m["success"].(bool); ok && !v {
		return true
	}
	if v, ok := m["status"].(string); ok && strings.EqualFold(v, "error") {
		return true
	}
	return false
}

// ─── small helpers ─────────────────────────────────────────

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// redactRawJSON decodes raw and replaces every string leaf with a {sha256,len}
// summary unless captureRaw is set. Mirrors collector/redact.go.
func redactRawJSON(raw json.RawMessage, captureRaw bool) any {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return redactJSONValue(string(raw), captureRaw)
	}
	return redactJSONValue(decoded, captureRaw)
}

func redactJSONValue(v any, captureRaw bool) any {
	switch t := v.(type) {
	case string:
		if captureRaw {
			return t
		}
		return map[string]any{"sha256": sha256Hex(t), "len": len(t)}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = redactJSONValue(vv, captureRaw)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = redactJSONValue(vv, captureRaw)
		}
		return out
	default:
		return t
	}
}

func stringFieldOf(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func putNonEmpty(md map[string]interface{}, key, val string) {
	if val != "" {
		md[key] = val
	}
}

// normalizeEventName lowercases an unknown hook event name for use as an action
// suffix, falling back to "event" when the name is empty.
func normalizeEventName(name string) string {
	if name == "" {
		return "event"
	}
	return strings.ToLower(name)
}
