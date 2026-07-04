package main

import "encoding/json"

// HookInput is the JSON payload Claude Code writes to the hook's stdin.
// Field presence varies by hook event; absent fields stay at their zero value.
type HookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"` // PostToolUse result (recent Claude Code uses tool_response)
	ToolUseID      string          `json:"tool_use_id"`   // correlates Pre/PostToolUse when present
	Prompt         string          `json:"prompt"`
	Source         string          `json:"source"`
	Message        string          `json:"message"`
	AgentType      string          `json:"agent_type"`
	AgentID        string          `json:"agent_id"`
	Reason         string          `json:"reason"`

	// Best-effort cost/usage fields carried by newer Claude Code PostToolUse
	// payloads. All optional and version-tolerant; absent on older versions.
	TotalCostUSD float64         `json:"total_cost_usd,omitempty"`
	DurationMs   int64           `json:"duration_ms,omitempty"`
	Usage        json.RawMessage `json:"usage,omitempty"`
}
