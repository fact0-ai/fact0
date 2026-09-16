package audit

import (
	"context"
	"time"
)

// ClaudeCodeSession is one aggregated Claude Code coding session, rolled up
// server-side from the claude_code.* audit event stream plus the telemetry
// spans of the linked execution. It backs the dashboard "Coding Agents" view.
type ClaudeCodeSession struct {
	SessionID      string     `json:"session_id"`
	Cwd            string     `json:"cwd"`
	Source         string     `json:"source"`
	PermissionMode string     `json:"permission_mode"`
	ActorID        string     `json:"actor_id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	DurationS      float64    `json:"duration_s"`
	Status         string     `json:"status"` // "active" | "idle" | "completed"
	Prompts        int        `json:"prompts"`
	ToolCalls      int        `json:"tool_calls"`
	FilesTouched   int        `json:"files_touched"`
	Commands       int        `json:"commands"`
	Failures       int        `json:"failures"`
	GitBranch      string     `json:"git_branch,omitempty"`
	Denied         int        `json:"denied"`
	CostUSD        float64    `json:"cost_usd"`
	Tokens         int64      `json:"tokens"`
	ExecutionID    string     `json:"execution_id,omitempty"`
	LastActivityAt time.Time  `json:"last_activity_at"`
}

// ClaudeCodeSessionFilter scopes a session list query. Zero To means "now";
// zero From means 30 days before To (bounding the aggregation scan).
type ClaudeCodeSessionFilter struct {
	From     time.Time
	To       time.Time
	Cwd      string
	Page     int
	PageSize int
}

// Normalise applies pagination defaults, mirroring audit.Filter.
func (f *ClaudeCodeSessionFilter) Normalise() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
}

// Offset returns the SQL offset corresponding to Page/PageSize.
func (f ClaudeCodeSessionFilter) Offset() int { return (f.Page - 1) * f.PageSize }

// ClaudeCodeSessionStore aggregates Claude Code sessions. Implemented by
// postgres.AuditStore.
type ClaudeCodeSessionStore interface {
	ListClaudeCodeSessions(ctx context.Context, tenantID string, f ClaudeCodeSessionFilter) ([]*ClaudeCodeSession, int, error)
	GetClaudeCodeSession(ctx context.Context, tenantID, sessionID string) (*ClaudeCodeSession, error)
}

// ClaudeCodeRepoCount is one repo's session count within a summary window.
type ClaudeCodeRepoCount struct {
	Cwd      string `json:"cwd"`
	Sessions int    `json:"sessions"`
}

// ClaudeCodeWeeklySummary is the tenant-level rollup behind the weekly digest.
type ClaudeCodeWeeklySummary struct {
	Sessions     int                   `json:"sessions"`
	Prompts      int                   `json:"prompts"`
	ToolCalls    int                   `json:"tool_calls"`
	FilesTouched int                   `json:"files_touched"` // distinct files tenant-wide
	Commands     int                   `json:"commands"`
	Denied       int                   `json:"denied"`
	CostUSD      float64               `json:"cost_usd"`
	Tokens       int64                 `json:"tokens"`
	TopRepos     []ClaudeCodeRepoCount `json:"top_repos"`
}
