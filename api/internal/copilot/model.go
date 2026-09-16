// Package copilot defines session + transcript persistence for the
// dashboard chat assistant. It is used to store the session and transcript
package copilot

import (
	"encoding/json"
	"time"
)

const (
	ResourceTypeSession = "copilot_session"
	ActorCopilot        = "fact0-copilot"

	ActionSessionStarted = "copilot.session.started"
	ActionQuerySubmitted = "copilot.query.submitted"
	ActionToolInvoked    = "copilot.tool.invoked"
	ActionToolCompleted  = "copilot.tool.completed"
	ActionNavigate       = "copilot.navigate"
)

// Session is a per-user copilot thread scoped to a tenant.
type Session struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	OrgID     string    `json:"org_id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message is a single turn in a copilot session transcript.
type Message struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	TenantID  string          `json:"tenant_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Parts     json.RawMessage `json:"parts,omitempty"`
	Redacted  bool            `json:"redacted"`
	CreatedAt time.Time       `json:"created_at"`
}

// MessageInput is the write shape for appending transcript rows.
type MessageInput struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content string          `json:"content"`
	Parts   json.RawMessage `json:"parts,omitempty"`
}

// ActivityInput carries metadata-only copilot telemetry (stored in
// copilot_activity - NOT in compliance audit_events).
type ActivityInput struct {
	Action    string                 `json:"action"`
	SessionID string                 `json:"session_id"`
	Outcome   string                 `json:"outcome,omitempty"`
	ActorType string                 `json:"actor_type,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Activity is a single copilot telemetry row.
type Activity struct {
	ID        string                 `json:"id"`
	TenantID  string                 `json:"tenant_id"`
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	Action    string                 `json:"action"`
	ActorType string                 `json:"actor_type"`
	Outcome   string                 `json:"outcome"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// AuditInput is an alias kept for handler JSON compatibility.
type AuditInput = ActivityInput

// CopilotQuota is the quota status returned to the frontend.
type CopilotQuota struct {
	Used     int    `json:"used"`
	Limit    *int   `json:"limit"`     // nil = unlimited
	ResetsAt string `json:"resets_at"` // first day of next month, UTC
	Plan     string `json:"plan"`
}
