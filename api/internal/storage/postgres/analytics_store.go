package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fact0-ai/fact0/internal/analytics"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// GetLLMMetrics fetches aggregated LLM invocation metrics for a tenant.
func (s *Store) GetLLMMetrics(ctx context.Context, tenantID string, from, to time.Time) (*analytics.LLMMetrics, error) {
	// 1. Total/Success/Error counts and token aggregates.
	query := `
		SELECT
			COUNT(s.id) AS total_calls,
			COUNT(CASE WHEN s.status = 'COMPLETED' THEN 1 END) AS success_count,
			COUNT(CASE WHEN s.status = 'FAILED' THEN 1 END) AS error_count,
			COALESCE(SUM((s.detail->>'prompt_tokens')::bigint), 0) AS prompt_tokens,
			COALESCE(SUM((s.detail->>'completion_tokens')::bigint), 0) AS completion_tokens,
			COALESCE(SUM((s.detail->>'total_tokens')::bigint), 0) AS total_tokens,
			COALESCE(AVG(CASE WHEN s.ended_at IS NOT NULL THEN EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000 END), 0) AS avg_latency
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
	`

	var metrics analytics.LLMMetrics
	err := s.pool.QueryRow(ctx, query, tenantID, from, to).Scan(
		&metrics.TotalCalls,
		&metrics.SuccessCount,
		&metrics.ErrorCount,
		&metrics.TotalPromptTokens,
		&metrics.TotalCompletionTokens,
		&metrics.TotalTokens,
		&metrics.AvgLatencyMs,
	)
	if err != nil {
		return nil, apperrors.Internal("querying llm metrics", err)
	}

	if metrics.TotalCalls > 0 {
		metrics.SuccessRate = float64(metrics.SuccessCount) / float64(metrics.TotalCalls)
	}

	// 2. Percentiles using aggregate query.
	pctQuery := `
		SELECT
			COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000), 0) AS p50,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000), 0) AS p95,
			COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000), 0) AS p99
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		  AND s.ended_at IS NOT NULL
	`
	err = s.pool.QueryRow(ctx, pctQuery, tenantID, from, to).Scan(
		&metrics.P50LatencyMs,
		&metrics.P95LatencyMs,
		&metrics.P99LatencyMs,
	)
	if err != nil {
		return nil, apperrors.Internal("querying llm percentiles", err)
	}

	// 3. Breakdown by model.
	modelQuery := `
		SELECT
			COALESCE(s.detail->>'model_name', 'unknown') AS model_name,
			COALESCE(s.detail->>'model_provider', 'unknown') AS model_provider,
			COUNT(s.id) AS call_count,
			COALESCE(AVG(CASE WHEN s.ended_at IS NOT NULL THEN EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000 END), 0) AS avg_latency,
			COALESCE(SUM((s.detail->>'total_tokens')::bigint), 0) AS total_tokens,
			COALESCE(SUM((s.detail->>'prompt_tokens')::bigint), 0) AS prompt_tokens,
			COALESCE(SUM((s.detail->>'completion_tokens')::bigint), 0) AS completion_tokens,
			COUNT(CASE WHEN s.status = 'FAILED' THEN 1 END) AS error_count
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY model_name, model_provider
		ORDER BY call_count DESC
	`
	rows, err := s.pool.Query(ctx, modelQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying llm model breakdown", err)
	}
	defer rows.Close()

	for rows.Next() {
		var mb analytics.ModelBreakdown
		err := rows.Scan(
			&mb.ModelName,
			&mb.ModelProvider,
			&mb.CallCount,
			&mb.AvgLatencyMs,
			&mb.TotalTokens,
			&mb.PromptTokens,
			&mb.CompletionTokens,
			&mb.ErrorCount,
		)
		if err != nil {
			return nil, apperrors.Internal("scanning model breakdown", err)
		}
		// Populate cost
		mb.EstimatedCostUSD = analytics.EstimateCost(mb.ModelName, mb.ModelProvider, int32(mb.PromptTokens), int32(mb.CompletionTokens))
		metrics.ByModel = append(metrics.ByModel, &mb)
		metrics.EstimatedCostUSD += mb.EstimatedCostUSD
	}

	// 4. Time series points (default daily bucket).
	tsQuery := `
		SELECT
			TO_CHAR(s.started_at, 'YYYY-MM-DD') AS bucket,
			COUNT(s.id)::double precision AS val
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	tsRows, err := s.pool.Query(ctx, tsQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying llm time series", err)
	}
	defer tsRows.Close()

	for tsRows.Next() {
		var pt analytics.TimeSeriesPoint
		if err := tsRows.Scan(&pt.Bucket, &pt.Value); err != nil {
			return nil, apperrors.Internal("scanning llm time series point", err)
		}
		metrics.TimeSeries = append(metrics.TimeSeries, pt)
	}

	// 5. Token time series.
	tokQuery := `
		SELECT
			TO_CHAR(s.started_at, 'YYYY-MM-DD') AS bucket,
			COALESCE(SUM((s.detail->>'prompt_tokens')::bigint), 0) AS prompt_tokens,
			COALESCE(SUM((s.detail->>'completion_tokens')::bigint), 0) AS completion_tokens,
			COALESCE(SUM((s.detail->>'total_tokens')::bigint), 0) AS total_tokens
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	tokRows, err := s.pool.Query(ctx, tokQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying token time series", err)
	}
	defer tokRows.Close()

	for tokRows.Next() {
		var tpt analytics.TimeSeriesTokens
		if err := tokRows.Scan(&tpt.Bucket, &tpt.PromptTokens, &tpt.CompletionTokens, &tpt.TotalTokens); err != nil {
			return nil, apperrors.Internal("scanning token time series point", err)
		}
		metrics.TokenTimeSeries = append(metrics.TokenTimeSeries, tpt)
	}

	return &metrics, nil
}

// GetToolMetrics fetches aggregated tool invocation metrics for a tenant.
func (s *Store) GetToolMetrics(ctx context.Context, tenantID string, from, to time.Time) (*analytics.ToolMetrics, error) {
	query := `
		SELECT
			COUNT(s.id) AS total_calls,
			COUNT(CASE WHEN s.status = 'COMPLETED' THEN 1 END) AS success_count,
			COUNT(CASE WHEN s.status = 'FAILED' THEN 1 END) AS error_count,
			COALESCE(AVG(CASE WHEN s.ended_at IS NOT NULL THEN EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000 END), 0) AS avg_duration
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'TOOL_CALL'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
	`

	var metrics analytics.ToolMetrics
	err := s.pool.QueryRow(ctx, query, tenantID, from, to).Scan(
		&metrics.TotalCalls,
		&metrics.SuccessCount,
		&metrics.ErrorCount,
		&metrics.AvgDurationMs,
	)
	if err != nil {
		return nil, apperrors.Internal("querying tool metrics", err)
	}

	if metrics.TotalCalls > 0 {
		metrics.SuccessRate = float64(metrics.SuccessCount) / float64(metrics.TotalCalls)
	}

	// Breakdown by tool.
	toolQuery := `
		SELECT
			COALESCE(s.detail->>'tool_name', 'unknown') AS tool_name,
			COUNT(s.id) AS call_count,
			COALESCE(AVG(CASE WHEN s.ended_at IS NOT NULL THEN EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) * 1000 END), 0) AS avg_duration,
			COUNT(CASE WHEN s.status = 'FAILED' THEN 1 END) AS error_count
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'TOOL_CALL'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY tool_name
		ORDER BY call_count DESC
	`
	rows, err := s.pool.Query(ctx, toolQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying tool breakdown", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tb analytics.ToolBreakdown
		err := rows.Scan(
			&tb.ToolName,
			&tb.CallCount,
			&tb.AvgDurationMs,
			&tb.ErrorCount,
		)
		if err != nil {
			return nil, apperrors.Internal("scanning tool breakdown", err)
		}
		if tb.CallCount > 0 {
			tb.SuccessRate = float64(tb.CallCount-tb.ErrorCount) / float64(tb.CallCount)
		}
		metrics.ByTool = append(metrics.ByTool, &tb)
	}

	// Tool time series.
	tsQuery := `
		SELECT
			TO_CHAR(s.started_at, 'YYYY-MM-DD') AS bucket,
			COUNT(s.id)::double precision AS val
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'TOOL_CALL'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	tsRows, err := s.pool.Query(ctx, tsQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying tool time series", err)
	}
	defer tsRows.Close()

	for tsRows.Next() {
		var pt analytics.TimeSeriesPoint
		if err := tsRows.Scan(&pt.Bucket, &pt.Value); err != nil {
			return nil, apperrors.Internal("scanning tool time series point", err)
		}
		metrics.TimeSeries = append(metrics.TimeSeries, pt)
	}

	return &metrics, nil
}

// GetErrorMetrics gets error count breakdowns.
func (s *Store) GetErrorMetrics(ctx context.Context, tenantID string, from, to time.Time) (*analytics.ErrorBreakdown, error) {
	var metrics analytics.ErrorBreakdown

	// 1. Group errors by model
	modelErrQuery := `
		SELECT
			COALESCE(s.detail->>'model_name', 'unknown') AS name,
			COUNT(s.id) AS error_count
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND s.status = 'FAILED'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY 1
		ORDER BY error_count DESC
	`
	rows, err := s.pool.Query(ctx, modelErrQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying model errors", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eg analytics.ErrorGroup
		if err := rows.Scan(&eg.Name, &eg.ErrorCount); err == nil {
			eg.SpanType = "MODEL_INVOCATION"
			metrics.ByModel = append(metrics.ByModel, &eg)
			metrics.TotalErrors += eg.ErrorCount
		}
	}

	// 2. Group errors by tool
	toolErrQuery := `
		SELECT
			COALESCE(s.detail->>'tool_name', 'unknown') AS name,
			COUNT(s.id) AS error_count
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'TOOL_CALL'
		  AND s.status = 'FAILED'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY 1
		ORDER BY error_count DESC
	`
	tRows, err := s.pool.Query(ctx, toolErrQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying tool errors", err)
	}
	defer tRows.Close()

	for tRows.Next() {
		var eg analytics.ErrorGroup
		if err := tRows.Scan(&eg.Name, &eg.ErrorCount); err == nil {
			eg.SpanType = "TOOL_CALL"
			metrics.ByTool = append(metrics.ByTool, &eg)
			metrics.TotalErrors += eg.ErrorCount
		}
	}

	// 3. Error time series
	tsQuery := `
		SELECT
			TO_CHAR(s.started_at, 'YYYY-MM-DD') AS bucket,
			COUNT(s.id)::double precision AS val
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.status = 'FAILED'
		  AND s.started_at >= $2
		  AND s.started_at <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	tsRows, err := s.pool.Query(ctx, tsQuery, tenantID, from, to)
	if err != nil {
		return nil, apperrors.Internal("querying error time series", err)
	}
	defer tsRows.Close()

	for tsRows.Next() {
		var pt analytics.TimeSeriesPoint
		if err := tsRows.Scan(&pt.Bucket, &pt.Value); err == nil {
			metrics.TimeSeries = append(metrics.TimeSeries, pt)
		}
	}

	return &metrics, nil
}

// ListSessions groups executions by session_id in metadata (or fallback to execution sequence).
func (s *Store) ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]analytics.ConversationSession, error) {
	// We extract session_id from executions.metadata->>'session_id'.
	// If it doesn't exist, we fall back to using execution.id itself as session_id (each run is a session).
	query := `
		SELECT
			COALESCE(e.metadata->>'session_id', e.id) AS sess_id,
			e.tenant_id,
			e.agent_id,
			e.agent_name,
			MIN(e.started_at) AS started_at,
			MAX(COALESCE(e.ended_at, e.started_at)) AS last_active_at,
			COUNT(e.id)::int AS turn_count,
			COALESCE(SUM((s.detail->>'total_tokens')::bigint), 0) AS total_tokens,
			MAX(e.status) AS status
		FROM executions e
		LEFT JOIN spans s ON s.execution_id = e.id AND s.span_type = 'MODEL_INVOCATION'
		WHERE e.tenant_id = $1
		GROUP BY sess_id, e.tenant_id, e.agent_id, e.agent_name
		ORDER BY last_active_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := s.pool.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, apperrors.Internal("listing sessions", err)
	}
	defer rows.Close()

	var sessions []analytics.ConversationSession
	for rows.Next() {
		var sess analytics.ConversationSession
		var statusStr string
		err := rows.Scan(
			&sess.SessionID,
			&sess.TenantID,
			&sess.AgentID,
			&sess.AgentName,
			&sess.StartedAt,
			&sess.LastActiveAt,
			&sess.TurnCount,
			&sess.TotalTokens,
			&statusStr,
		)
		if err != nil {
			return nil, apperrors.Internal("scanning session", err)
		}
		sess.Status = statusStr

		// Fetch estimated cost by looking at the models used in this session.
		sess.TotalCostUSD = s.estimateSessionCost(ctx, tenantID, sess.SessionID)

		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (s *Store) estimateSessionCost(ctx context.Context, tenantID, sessionID string) float64 {
	query := `
		SELECT
			COALESCE(s.detail->>'model_name', '') AS model_name,
			COALESCE(s.detail->>'model_provider', '') AS provider,
			COALESCE((s.detail->>'prompt_tokens')::int, 0) AS prompt_tokens,
			COALESCE((s.detail->>'completion_tokens')::int, 0) AS completion_tokens
		FROM spans s
		JOIN executions e ON s.execution_id = e.id
		WHERE e.tenant_id = $1
		  AND s.span_type = 'MODEL_INVOCATION'
		  AND (e.metadata->>'session_id' = $2 OR (e.metadata->>'session_id' IS NULL AND e.id = $2))
	`
	rows, err := s.pool.Query(ctx, query, tenantID, sessionID)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var total float64
	for rows.Next() {
		var model, provider string
		var pt, ct int32
		if err := rows.Scan(&model, &provider, &pt, &ct); err == nil {
			total += analytics.EstimateCost(model, provider, pt, ct)
		}
	}
	return total
}

// GetSessionDetail returns session info and all execution turns.
func (s *Store) GetSessionDetail(ctx context.Context, tenantID, sessionID string) (*analytics.SessionDetail, error) {
	// First fetch session properties.
	query := `
		SELECT
			COALESCE(e.metadata->>'session_id', e.id) AS sess_id,
			e.tenant_id,
			e.agent_id,
			e.agent_name,
			MIN(e.started_at) AS started_at,
			MAX(COALESCE(e.ended_at, e.started_at)) AS last_active_at,
			COUNT(e.id)::int AS turn_count,
			COALESCE(SUM((s.detail->>'total_tokens')::bigint), 0) AS total_tokens,
			MAX(e.status) AS status
		FROM executions e
		LEFT JOIN spans s ON s.execution_id = e.id AND s.span_type = 'MODEL_INVOCATION'
		WHERE e.tenant_id = $1
		  AND (e.metadata->>'session_id' = $2 OR (e.metadata->>'session_id' IS NULL AND e.id = $2))
		GROUP BY sess_id, e.tenant_id, e.agent_id, e.agent_name
	`
	var sess analytics.ConversationSession
	var statusStr string
	err := s.pool.QueryRow(ctx, query, tenantID, sessionID).Scan(
		&sess.SessionID,
		&sess.TenantID,
		&sess.AgentID,
		&sess.AgentName,
		&sess.StartedAt,
		&sess.LastActiveAt,
		&sess.TurnCount,
		&sess.TotalTokens,
		&statusStr,
	)
	if err == pgx.ErrNoRows {
		return nil, apperrors.NotFound("session", sessionID)
	}
	if err != nil {
		return nil, apperrors.Internal("querying session properties", err)
	}
	sess.Status = statusStr
	sess.TotalCostUSD = s.estimateSessionCost(ctx, tenantID, sessionID)

	// Now fetch all executions (turns) belonging to this session.
	turnQuery := `
		SELECT
			e.id,
			e.agent_id,
			e.agent_name,
			e.status,
			e.started_at,
			e.ended_at,
			COALESCE((e.metadata->>'turn_sequence')::int, 0) AS seq
		FROM executions e
		WHERE e.tenant_id = $1
		  AND (e.metadata->>'session_id' = $2 OR (e.metadata->>'session_id' IS NULL AND e.id = $2))
		ORDER BY seq ASC, e.started_at ASC
	`
	rows, err := s.pool.Query(ctx, turnQuery, tenantID, sessionID)
	if err != nil {
		return nil, apperrors.Internal("querying session turns", err)
	}
	defer rows.Close()

	var turns []analytics.TurnTrace
	for rows.Next() {
		var turn analytics.TurnTrace
		var turnStatus string
		err := rows.Scan(
			&turn.ExecutionID,
			&turn.AgentID,
			&turn.AgentName,
			&turnStatus,
			&turn.StartedAt,
			&turn.EndedAt,
			&turn.Sequence,
		)
		if err != nil {
			return nil, apperrors.Internal("scanning turn", err)
		}
		turn.Status = turnStatus
		if turn.EndedAt != nil {
			turn.DurationMs = turn.EndedAt.Sub(turn.StartedAt).Milliseconds()
		}

		// Fetch LLM summaries for this turn
		llmQuery := `
			SELECT id, name, status,
				COALESCE(detail->>'model_name', '') AS model,
				COALESCE(detail->>'model_provider', '') AS provider,
				COALESCE((detail->>'prompt_tokens')::int, 0) AS prompt_t,
				COALESCE((detail->>'completion_tokens')::int, 0) AS comp_t,
				COALESCE((detail->>'total_tokens')::int, 0) AS total_t,
				COALESCE((detail->>'latency_ms')::bigint, 0) AS lat
			FROM spans
			WHERE execution_id = $1 AND span_type = 'MODEL_INVOCATION'
			ORDER BY started_at ASC
		`
		lRows, err := s.pool.Query(ctx, llmQuery, turn.ExecutionID)
		if err == nil {
			for lRows.Next() {
				var lSum analytics.LLMCallSummary
				var model, provider string
				var promptT, compT, totalT int32
				err := lRows.Scan(&lSum.SpanID, &lSum.SpanID, &lSum.Status, &model, &provider, &promptT, &compT, &totalT, &lSum.LatencyMs)
				if err == nil {
					lSum.ModelName = model
					lSum.TotalTokens = totalT
					turn.LLMCalls = append(turn.LLMCalls, lSum)
					turn.TotalTokens += int64(totalT)
					turn.TotalCostUSD += analytics.EstimateCost(model, provider, promptT, compT)
				}
			}
			lRows.Close()
		}

		// Fetch Tool summaries for this turn
		toolQuery := `
			SELECT id, status,
				COALESCE(detail->>'tool_name', '') AS tool,
				COALESCE((detail->>'duration_ms')::bigint, 0) AS dur
			FROM spans
			WHERE execution_id = $1 AND span_type = 'TOOL_CALL'
			ORDER BY started_at ASC
		`
		tRows, err := s.pool.Query(ctx, toolQuery, turn.ExecutionID)
		if err == nil {
			for tRows.Next() {
				var tSum analytics.ToolCallSummary
				var tool string
				err := tRows.Scan(&tSum.SpanID, &tSum.Status, &tool, &tSum.DurationMs)
				if err == nil {
					tSum.ToolName = tool
					turn.ToolCalls = append(turn.ToolCalls, tSum)
				}
			}
			tRows.Close()
		}

		turns = append(turns, turn)
	}

	return &analytics.SessionDetail{
		Session: sess,
		Turns:   turns,
	}, nil
}

// ─── Prompt Catalog SQL Operations ──────────────────────────

// InsertPrompt inserts a prompt record version into the database.
func (s *Store) InsertPrompt(ctx context.Context, p *analytics.PromptRecord) error {
	meta, err := json.Marshal(p.Metadata)
	if err != nil {
		return apperrors.Internal("marshaling prompt metadata", err)
	}

	query := `
		INSERT INTO prompt_catalog (id, tenant_id, name, version, template, variables, model_hints, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, name, version) DO NOTHING
	`
	_, err = s.pool.Exec(ctx, query,
		p.ID, p.TenantID, p.Name, p.Version, p.Template, p.Variables, p.ModelHints, string(meta), p.CreatedAt,
	)
	if err != nil {
		return apperrors.Internal("inserting prompt", err)
	}
	return nil
}

// GetPromptLatestVersion gets the highest version number for a prompt.
func (s *Store) GetPromptLatestVersion(ctx context.Context, tenantID, name string) (int, error) {
	query := `SELECT COALESCE(MAX(version), 0) FROM prompt_catalog WHERE tenant_id = $1 AND name = $2`
	var version int
	err := s.pool.QueryRow(ctx, query, tenantID, name).Scan(&version)
	if err != nil {
		return 0, apperrors.Internal("querying latest prompt version", err)
	}
	return version, nil
}

// ListPrompts lists the prompt catalog with aggregated usage metrics.
func (s *Store) ListPrompts(ctx context.Context, tenantID string) ([]analytics.PromptRecord, error) {
	// We join with spans where metadata has prompt_name matches prompt_catalog.name.
	// To perform aggregation properly, we query prompt metadata usage.
	query := `
		SELECT
			p.id, p.tenant_id, p.name, p.version, p.template, p.variables, p.model_hints, p.metadata, p.created_at,
			COUNT(s.id) AS usage_count,
			COALESCE(AVG((s.detail->>'total_tokens')::bigint), 0)::double precision AS avg_tokens,
			COALESCE(AVG((s.detail->>'latency_ms')::bigint), 0)::double precision AS avg_latency
		FROM prompt_catalog p
		LEFT JOIN spans s ON s.span_type = 'MODEL_INVOCATION' AND (s.metadata->>'prompt_name' = p.name OR s.metadata->>'prompt_id' = p.id)
		WHERE p.tenant_id = $1
		GROUP BY p.id, p.tenant_id, p.name, p.version, p.template, p.variables, p.model_hints, p.metadata, p.created_at
		ORDER BY p.name ASC, p.version DESC
	`
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, apperrors.Internal("listing prompts", err)
	}
	defer rows.Close()

	var prompts []analytics.PromptRecord
	for rows.Next() {
		var p analytics.PromptRecord
		var meta []byte
		err := rows.Scan(
			&p.ID,
			&p.TenantID,
			&p.Name,
			&p.Version,
			&p.Template,
			&p.Variables,
			&p.ModelHints,
			&meta,
			&p.CreatedAt,
			&p.UsageCount,
			&p.AvgTokens,
			&p.AvgLatencyMs,
		)
		if err != nil {
			return nil, apperrors.Internal("scanning prompt", err)
		}
		_ = json.Unmarshal(meta, &p.Metadata)
		prompts = append(prompts, p)
	}
	return prompts, nil
}

// GetPrompt retrieves a specific prompt version details.
func (s *Store) GetPrompt(ctx context.Context, tenantID, id string) (*analytics.PromptRecord, error) {
	query := `
		SELECT
			p.id, p.tenant_id, p.name, p.version, p.template, p.variables, p.model_hints, p.metadata, p.created_at,
			COUNT(s.id) AS usage_count,
			COALESCE(AVG((s.detail->>'total_tokens')::bigint), 0)::double precision AS avg_tokens,
			COALESCE(AVG((s.detail->>'latency_ms')::bigint), 0)::double precision AS avg_latency
		FROM prompt_catalog p
		LEFT JOIN spans s ON s.span_type = 'MODEL_INVOCATION' AND (s.metadata->>'prompt_name' = p.name OR s.metadata->>'prompt_id' = p.id)
		WHERE p.tenant_id = $1 AND p.id = $2
		GROUP BY p.id, p.tenant_id, p.name, p.version, p.template, p.variables, p.model_hints, p.metadata, p.created_at
	`
	var p analytics.PromptRecord
	var meta []byte
	err := s.pool.QueryRow(ctx, query, tenantID, id).Scan(
		&p.ID,
		&p.TenantID,
		&p.Name,
		&p.Version,
		&p.Template,
		&p.Variables,
		&p.ModelHints,
		&meta,
		&p.CreatedAt,
		&p.UsageCount,
		&p.AvgTokens,
		&p.AvgLatencyMs,
	)
	if err == pgx.ErrNoRows {
		return nil, apperrors.NotFound("prompt", id)
	}
	if err != nil {
		return nil, apperrors.Internal("getting prompt", err)
	}
	_ = json.Unmarshal(meta, &p.Metadata)
	return &p, nil
}
