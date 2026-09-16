package digest

import (
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
)

// WebhookPayload is the JSON shape POSTed to the tenant's alert webhook.
type WebhookPayload struct {
	Type        string                        `json:"type"` // "coding_agents_digest"
	TenantID    string                        `json:"tenant_id"`
	PeriodStart time.Time                     `json:"period_start"`
	PeriodEnd   time.Time                     `json:"period_end"`
	Stats       audit.ClaudeCodeWeeklySummary `json:"stats"`
}

func webhookPayload(c candidate, from, to time.Time, s *audit.ClaudeCodeWeeklySummary) WebhookPayload {
	return WebhookPayload{
		Type:        "coding_agents_digest",
		TenantID:    c.TenantID,
		PeriodStart: from,
		PeriodEnd:   to,
		Stats:       *s,
	}
}

// emailProps matches the camelCase prop style of the other EmailBridge
// templates (quota-alert, report-ready). Requires a "cc-weekly-digest"
// template on the email-bridge side.
func emailProps(c candidate, from, to time.Time, s *audit.ClaudeCodeWeeklySummary) map[string]any {
	name := c.TenantName
	if name == "" {
		name = c.TenantID
	}
	return map[string]any{
		"workspaceName": name,
		"periodStart":   from.Format("January 2, 2006"),
		"periodEnd":     to.Format("January 2, 2006"),
		"sessions":      s.Sessions,
		"prompts":       s.Prompts,
		"toolCalls":     s.ToolCalls,
		"filesTouched":  s.FilesTouched,
		"commands":      s.Commands,
		"denied":        s.Denied,
		"costUsd":       s.CostUSD,
		"tokens":        s.Tokens,
		"topRepos":      s.TopRepos,
	}
}
