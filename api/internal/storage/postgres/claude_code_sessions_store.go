package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// sessionIdleAfter is how long a session without a session.end event may be
// silent before it stops reading as "active".
const sessionIdleAfter = 30 * time.Minute

// claudeCodeSessionsSQL aggregates claude_code.* audit events into per-session
// rollups, then attaches span-derived stats (policy denials, cost, tokens) for
// the page only via LEFT JOIN LATERAL.
//
// Single statement by design: paginating the audit rollup first means the
// (potentially expensive) span join runs for one page of sessions, not all of
// them, and COUNT(*) OVER () yields the total without re-running the scan.
// The scan is bounded by tenant + action prefix + time window and is served by
// idx_audit_tenant_time / idx_audit_tenant_action; no dedicated index is
// needed until per-tenant claude-code volume reaches ~1M rows (then add a
// partial index on (tenant_id, (metadata->>'session_id'))).
//
// Placeholders: $1 tenant, $2 from, $3 to, $4 session filter (” = all),
// $5 cwd filter (” = all), $6 limit, $7 offset.
const claudeCodeSessionsSQL = `
WITH ev AS (
    SELECT CASE WHEN resource_type = 'claude_code.session'
                THEN resource_id
                ELSE metadata->>'session_id' END AS session_key,
           action, actor_id, resource_id, resource_type, outcome, timestamp, metadata
    FROM audit_events
    WHERE tenant_id = $1
      AND action LIKE 'claude_code.%'
      AND timestamp >= $2 AND timestamp <= $3
),
sess AS (
    SELECT session_key AS session_id,
           COALESCE(MIN(timestamp) FILTER (WHERE action = 'claude_code.session.start'), MIN(timestamp)) AS started_at,
           MAX(timestamp) FILTER (WHERE action = 'claude_code.session.end')                             AS ended_at,
           MAX(timestamp)                                                                               AS last_activity_at,
           COALESCE(MAX(metadata->>'cwd'), '')                                                          AS cwd,
           COALESCE(MAX(metadata->>'source') FILTER (WHERE action = 'claude_code.session.start'), '')   AS source,
           COALESCE(MAX(metadata->>'permission_mode') FILTER (WHERE action = 'claude_code.session.start'), '') AS permission_mode,
           COALESCE(MAX(metadata->>'execution_id'), '')                                                 AS execution_id,
           COALESCE(MAX(actor_id) FILTER (WHERE action = 'claude_code.session.start'), MAX(actor_id))   AS actor_id,
           COUNT(*) FILTER (WHERE action = 'claude_code.prompt.submit')                                 AS prompts,
           COUNT(*) FILTER (WHERE action LIKE 'claude_code.tool.%')                                     AS tool_calls,
           COUNT(DISTINCT resource_id) FILTER (WHERE action LIKE 'claude_code.tool.%' AND resource_type = 'file')          AS files_touched,
           COUNT(*) FILTER (WHERE action LIKE 'claude_code.tool.%' AND resource_type = 'shell.command')                    AS commands,
           COUNT(*) FILTER (WHERE action LIKE 'claude_code.tool.%' AND outcome IN ('error', 'failure'))                    AS failures,
           COALESCE(MAX(metadata->>'git_branch'), '')                                                                      AS git_branch
    FROM ev
    WHERE session_key IS NOT NULL AND session_key <> ''
      AND ($4 = '' OR session_key = $4)
    GROUP BY session_key
),
paged AS (
    SELECT *, COUNT(*) OVER () AS total
    FROM sess
    WHERE ($5 = '' OR cwd = $5)
    ORDER BY last_activity_at DESC
    LIMIT $6 OFFSET $7
)
SELECT p.session_id, p.started_at, p.ended_at, p.last_activity_at,
       p.cwd, p.source, p.permission_mode, p.execution_id, p.actor_id,
       p.prompts, p.tool_calls, p.files_touched, p.commands, p.failures,
       p.git_branch, p.total,
       COALESCE(r.denied, 0)   AS denied,
       COALESCE(r.cost_usd, 0) AS cost_usd,
       COALESCE(r.tokens, 0)   AS tokens
FROM paged p
LEFT JOIN LATERAL (
    SELECT COUNT(*) FILTER (WHERE sp.span_type = 'POLICY_EVALUATION' AND sp.metadata->>'decision' = 'deny') AS denied,
           COALESCE(SUM((sp.detail->>'cost_usd')::double precision) FILTER (WHERE sp.span_type = 'MODEL_INVOCATION'), 0) AS cost_usd,
           COALESCE(SUM(COALESCE(NULLIF((sp.detail->>'total_tokens')::bigint, 0),
                                 COALESCE((sp.detail->>'prompt_tokens')::bigint, 0)
                               + COALESCE((sp.detail->>'completion_tokens')::bigint, 0)))
                    FILTER (WHERE sp.span_type = 'MODEL_INVOCATION'), 0) AS tokens
    FROM spans sp
    JOIN executions e ON e.id = sp.execution_id
    -- tenant guard: a forged execution_id in event metadata must not leak
    -- another tenant's span stats.
    WHERE sp.execution_id = p.execution_id AND p.execution_id <> '' AND e.tenant_id = $1
) r ON true
ORDER BY p.last_activity_at DESC`

// ListClaudeCodeSessions returns aggregated Claude Code sessions for a tenant,
// newest activity first.
func (s *AuditStore) ListClaudeCodeSessions(ctx context.Context, tenantID string, f audit.ClaudeCodeSessionFilter) ([]*audit.ClaudeCodeSession, int, error) {
	f.Normalise()
	to := f.To
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from := f.From
	if from.IsZero() {
		from = to.Add(-30 * 24 * time.Hour)
	}

	rows, err := s.readDB.Query(ctx, claudeCodeSessionsSQL,
		tenantID, from, to, "", f.Cwd, f.PageSize, f.Offset())
	if err != nil {
		return nil, 0, apperrors.Internal("listing claude code sessions", err)
	}
	defer rows.Close()

	sessions, total, err := scanClaudeCodeSessions(rows)
	if err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

// GetClaudeCodeSession returns one aggregated session by its Claude Code
// session id. No lower time bound so deep links to old sessions resolve.
func (s *AuditStore) GetClaudeCodeSession(ctx context.Context, tenantID, sessionID string) (*audit.ClaudeCodeSession, error) {
	rows, err := s.readDB.Query(ctx, claudeCodeSessionsSQL,
		tenantID, time.Time{}, time.Now().UTC(), sessionID, "", 1, 0)
	if err != nil {
		return nil, apperrors.Internal("getting claude code session", err)
	}
	defer rows.Close()

	sessions, _, err := scanClaudeCodeSessions(rows)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, apperrors.NotFound("claude_code_session", sessionID)
	}
	return sessions[0], nil
}

// GetClaudeCodeWeeklySummary computes the tenant-level rollup behind the
// weekly digest: totals across all sessions in the window plus the top 3
// repos by session count. files_touched is distinct tenant-wide (not summed
// per session).
func (s *AuditStore) GetClaudeCodeWeeklySummary(ctx context.Context, tenantID string, from, to time.Time) (*audit.ClaudeCodeWeeklySummary, error) {
	const q = `
WITH ev AS (
    SELECT CASE WHEN resource_type = 'claude_code.session'
                THEN resource_id ELSE metadata->>'session_id' END AS session_key,
           action, resource_id, resource_type, metadata
    FROM audit_events
    WHERE tenant_id = $1 AND action LIKE 'claude_code.%'
      AND timestamp >= $2 AND timestamp <= $3
),
sess AS (
    SELECT session_key,
           COALESCE(MAX(metadata->>'cwd'), '')          AS cwd,
           COALESCE(MAX(metadata->>'execution_id'), '') AS execution_id
    FROM ev
    WHERE session_key IS NOT NULL AND session_key <> ''
    GROUP BY session_key
),
totals AS (
    SELECT COUNT(*) FILTER (WHERE action = 'claude_code.prompt.submit')                                    AS prompts,
           COUNT(*) FILTER (WHERE action LIKE 'claude_code.tool.%')                                        AS tool_calls,
           COUNT(DISTINCT resource_id) FILTER (WHERE action LIKE 'claude_code.tool.%'
                                                 AND resource_type = 'file')                               AS files_touched,
           COUNT(*) FILTER (WHERE action LIKE 'claude_code.tool.%' AND resource_type = 'shell.command')    AS commands
    FROM ev
),
span_stats AS (
    SELECT COUNT(*) FILTER (WHERE sp.span_type = 'POLICY_EVALUATION' AND sp.metadata->>'decision' = 'deny') AS denied,
           COALESCE(SUM((sp.detail->>'cost_usd')::double precision)
                    FILTER (WHERE sp.span_type = 'MODEL_INVOCATION'), 0)                                    AS cost_usd,
           COALESCE(SUM(COALESCE(NULLIF((sp.detail->>'total_tokens')::bigint, 0),
                                 COALESCE((sp.detail->>'prompt_tokens')::bigint, 0)
                               + COALESCE((sp.detail->>'completion_tokens')::bigint, 0)))
                    FILTER (WHERE sp.span_type = 'MODEL_INVOCATION'), 0)                                    AS tokens
    FROM spans sp
    JOIN executions e ON e.id = sp.execution_id AND e.tenant_id = $1
    WHERE sp.execution_id IN (SELECT execution_id FROM sess WHERE execution_id <> '')
),
top_repos AS (
    SELECT COALESCE(jsonb_agg(jsonb_build_object('cwd', cwd, 'sessions', n) ORDER BY n DESC, cwd), '[]'::jsonb) AS repos
    FROM (SELECT cwd, COUNT(*) AS n FROM sess WHERE cwd <> ''
          GROUP BY cwd ORDER BY n DESC, cwd LIMIT 3) r
)
SELECT (SELECT COUNT(*) FROM sess), t.prompts, t.tool_calls, t.files_touched, t.commands,
       COALESCE(s.denied, 0), COALESCE(s.cost_usd, 0), COALESCE(s.tokens, 0), tr.repos
FROM totals t, span_stats s, top_repos tr`

	var (
		sum      audit.ClaudeCodeWeeklySummary
		rawRepos []byte
	)
	if err := s.readDB.QueryRow(ctx, q, tenantID, from, to).Scan(
		&sum.Sessions, &sum.Prompts, &sum.ToolCalls, &sum.FilesTouched, &sum.Commands,
		&sum.Denied, &sum.CostUSD, &sum.Tokens, &rawRepos,
	); err != nil {
		return nil, apperrors.Internal("computing claude code weekly summary", err)
	}
	if err := json.Unmarshal(rawRepos, &sum.TopRepos); err != nil {
		return nil, apperrors.Internal("decoding top repos", err)
	}
	if sum.TopRepos == nil {
		sum.TopRepos = []audit.ClaudeCodeRepoCount{}
	}
	return &sum, nil
}

// scanClaudeCodeSessions scans aggregation rows, deriving status and duration.
func scanClaudeCodeSessions(rows pgx.Rows) ([]*audit.ClaudeCodeSession, int, error) {
	out := []*audit.ClaudeCodeSession{}
	total := 0
	for rows.Next() {
		var (
			cs      audit.ClaudeCodeSession
			endedAt *time.Time
		)
		if err := rows.Scan(
			&cs.SessionID, &cs.StartedAt, &endedAt, &cs.LastActivityAt,
			&cs.Cwd, &cs.Source, &cs.PermissionMode, &cs.ExecutionID, &cs.ActorID,
			&cs.Prompts, &cs.ToolCalls, &cs.FilesTouched, &cs.Commands, &cs.Failures,
			&cs.GitBranch, &total,
			&cs.Denied, &cs.CostUSD, &cs.Tokens,
		); err != nil {
			return nil, 0, apperrors.Internal("scanning claude code session", err)
		}
		cs.EndedAt = endedAt
		switch {
		case endedAt != nil:
			cs.Status = "completed"
			cs.DurationS = endedAt.Sub(cs.StartedAt).Seconds()
		case time.Since(cs.LastActivityAt) > sessionIdleAfter:
			// Sessions that never fired SessionEnd (crash, kill) must not
			// read as active forever.
			cs.Status = "idle"
			cs.DurationS = cs.LastActivityAt.Sub(cs.StartedAt).Seconds()
		default:
			cs.Status = "active"
			cs.DurationS = cs.LastActivityAt.Sub(cs.StartedAt).Seconds()
		}
		out = append(out, &cs)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperrors.Internal("iterating claude code sessions", err)
	}
	return out, total, nil
}
