package audit

import (
	"context"
	"time"
)

// ClaudeCodePolicyRule is one governance rule evaluated by the collector's
// PreToolUse hook. JSON shape is byte-compatible with the collector's local
// PolicyRule so remote rules cache verbatim.
type ClaudeCodePolicyRule struct {
	Match  string `json:"match"`
	Tool   string `json:"tool,omitempty"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// ClaudeCodePolicy is a tenant's current governance policy. Version 0 is the
// sentinel for "no policy configured".
type ClaudeCodePolicy struct {
	Version   int                    `json:"version"`
	Enforce   bool                   `json:"enforce"`
	Rules     []ClaudeCodePolicyRule `json:"rules"`
	UpdatedAt time.Time              `json:"updated_at"`
	UpdatedBy string                 `json:"updated_by,omitempty"`
}

// ClaudeCodePolicyStore persists per-tenant governance policies.
// Implemented by postgres.AuditStore.
type ClaudeCodePolicyStore interface {
	// GetClaudeCodePolicy returns {Version: 0, Rules: []} (not an error) when
	// no policy exists for the tenant.
	GetClaudeCodePolicy(ctx context.Context, tenantID string) (*ClaudeCodePolicy, error)
	// PutClaudeCodePolicy is a compare-and-swap write: expectedVersion 0
	// creates the row; a mismatch returns apperrors.CodeConflict.
	PutClaudeCodePolicy(ctx context.Context, tenantID string, expectedVersion int,
		enforce bool, rules []ClaudeCodePolicyRule, updatedBy string) (*ClaudeCodePolicy, error)
}
