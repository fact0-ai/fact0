// Package postgres provides the PostgreSQL storage implementation.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/execution"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// Store implements persistent storage for execution data in PostgreSQL.
type Store struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewStore creates a new PostgreSQL store.
func NewStore(pool *pgxpool.Pool, logger zerolog.Logger) *Store {
	return &Store{pool: pool, logger: logger.With().Str("component", "postgres").Logger()}
}

// PoolOptions configures a pgx connection pool.
type PoolOptions struct {
	MaxConns int
	MinConns int
}

// Connect establishes a connection pool with default options (backward compat).
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return ConnectPool(ctx, dsn, PoolOptions{MaxConns: 25, MinConns: 1})
}

// ConnectPool establishes a connection pool to PostgreSQL.
//
// Simple query protocol is used for all queries so the pool is compatible
// with PgBouncer-style poolers in transaction mode (e.g. Neon, Supabase).
func ConnectPool(ctx context.Context, dsn string, opts PoolOptions) (*pgxpool.Pool, error) {
	dsn = sanitizeDSN(dsn)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres config: %w", err)
	}

	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	maxConns := opts.MaxConns
	if maxConns <= 0 {
		maxConns = 25
	}
	minConns := opts.MinConns
	if minConns <= 0 {
		minConns = 1
	}
	config.MaxConns = int32(maxConns)
	config.MinConns = int32(minConns)
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return pool, nil
}

// sanitizeDSN strips query parameters that are unsupported by pgx or that
// cause authentication failures when connecting through PgBouncer-style
// poolers (e.g. Neon's pooler endpoint, Supabase).
//
// Specifically:
//   - channel_binding=require - requires SCRAM-SHA-256 channel binding at the
//     TLS level; PgBouncer does not forward the TLS Finished message so the
//     handshake always fails. The direct (non-pooler) Neon endpoint supports
//     it, but the pooler does not. We strip it so the same DSN works for both.
//     pgx already negotiates SCRAM-SHA-256 by default; removing this param
//     does not weaken security.
func sanitizeDSN(dsn string) string {
	// Only try to sanitize URL-style DSNs (postgres:// / postgresql://).
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return dsn
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn // leave unchanged; ParseConfig will surface the error
	}
	q := u.Query()
	q.Del("channel_binding")
	u.RawQuery = q.Encode()
	return u.String()
}

// ─── Execution Operations ───────────────────────────────────

// InsertExecution persists a new execution scoped to tenantID.
func (s *Store) InsertExecution(ctx context.Context, tenantID string, e *execution.Execution) error {
	metadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return apperrors.Internal("marshaling metadata", err)
	}

	tag, err := s.pool.Exec(ctx, `
		INSERT INTO executions (id, tenant_id, agent_id, agent_name, status, root_span_id, trigger, started_at, ended_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		e.ID, tenantID, e.AgentID, e.AgentName, string(e.Status), e.RootSpanID, e.Trigger, e.StartedAt, e.EndedAt, string(metadata),
	)
	if err != nil {
		return apperrors.Internal("inserting execution", err)
	}
	if tag.RowsAffected() == 0 {
		existing, err := s.GetExecution(ctx, tenantID, e.ID)
		if err != nil {
			return apperrors.AlreadyExists("execution", e.ID)
		}
		if existing.AgentID != e.AgentID || existing.AgentName != e.AgentName || existing.Trigger != e.Trigger || !equalJSON(existing.Metadata, e.Metadata) {
			return apperrors.AlreadyExists("execution with different input", e.ID)
		}
		*e = *existing
	}
	return nil
}

// GetExecution retrieves an execution by ID scoped to tenantID.
func (s *Store) GetExecution(ctx context.Context, tenantID, id string) (*execution.Execution, error) {
	var e execution.Execution
	var status string
	var metadata []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, agent_name, status, root_span_id, trigger, started_at, ended_at, metadata, created_at
		FROM executions WHERE id = $1 AND tenant_id = $2`, id, tenantID,
	).Scan(&e.ID, &e.AgentID, &e.AgentName, &status, &e.RootSpanID, &e.Trigger, &e.StartedAt, &e.EndedAt, &metadata, &e.CreatedAt)

	if err == pgx.ErrNoRows {
		return nil, apperrors.NotFound("execution", id)
	}
	if err != nil {
		return nil, apperrors.Internal("getting execution", err)
	}

	e.Status = execution.Status(status)
	if err := unmarshalJSON(metadata, &e.Metadata); err != nil {
		return nil, apperrors.Internal("unmarshaling metadata", err)
	}

	return &e, nil
}

// UpdateExecutionStatus updates the status and end time of an execution.
func (s *Store) UpdateExecutionStatus(ctx context.Context, tenantID, id string, status execution.Status, endedAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE executions SET status = $3, ended_at = $4 WHERE id = $1 AND tenant_id = $2 AND status = 'RUNNING'`,
		id, tenantID, string(status), endedAt,
	)
	if err != nil {
		return apperrors.Internal("updating execution status", err)
	}
	if tag.RowsAffected() == 0 {
		existing, err := s.GetExecution(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if existing.Status != status {
			return apperrors.AlreadyExists("execution terminal status", id)
		}
	}
	return nil
}

// ListExecutions returns paginated executions for a tenant with optional filters.
func (s *Store) ListExecutions(ctx context.Context, tenantID, agentID, status string, limit, offset int) ([]*execution.Execution, int, error) {
	countSQL := `SELECT COUNT(*) FROM executions WHERE tenant_id = $1`
	querySQL := `SELECT id, agent_id, agent_name, status, root_span_id, trigger, started_at, ended_at, metadata, created_at FROM executions WHERE tenant_id = $1`

	args := []interface{}{tenantID}
	argIdx := 2

	if agentID != "" {
		filter := fmt.Sprintf(" AND agent_id = $%d", argIdx)
		countSQL += filter
		querySQL += filter
		args = append(args, agentID)
		argIdx++
	}
	if status != "" {
		filter := fmt.Sprintf(" AND status = $%d", argIdx)
		countSQL += filter
		querySQL += filter
		args = append(args, status)
		argIdx++
	}

	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, apperrors.Internal("counting executions", err)
	}

	querySQL += fmt.Sprintf(" ORDER BY started_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, apperrors.Internal("listing executions", err)
	}
	defer rows.Close()

	var results []*execution.Execution
	for rows.Next() {
		var e execution.Execution
		var statusStr string
		var metadata []byte
		if err := rows.Scan(&e.ID, &e.AgentID, &e.AgentName, &statusStr, &e.RootSpanID, &e.Trigger, &e.StartedAt, &e.EndedAt, &metadata, &e.CreatedAt); err != nil {
			return nil, 0, apperrors.Internal("scanning execution", err)
		}
		e.Status = execution.Status(statusStr)
		_ = unmarshalJSON(metadata, &e.Metadata)
		results = append(results, &e)
	}

	return results, total, nil
}

// ─── Span Operations ────────────────────────────────────────

// GetSpansByExecution retrieves all spans for an execution.
func (s *Store) GetSpansByExecution(ctx context.Context, executionID string) ([]*execution.Span, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, execution_id, parent_span_id, span_type, name, status,
		       started_at, ended_at, metadata, detail, error_info
		FROM spans WHERE execution_id = $1
		ORDER BY sequence_number ASC`, executionID)
	if err != nil {
		return nil, apperrors.Internal("listing spans", err)
	}
	defer rows.Close()

	var spans []*execution.Span
	for rows.Next() {
		span, err := s.scanSpan(rows)
		if err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}

	// Fetch all causality edges for the execution in one database roundtrip
	edgesRows, err := s.pool.Query(ctx, `
		SELECT sc.span_id, sc.caused_by_id
		FROM span_causality sc
		JOIN spans s ON sc.span_id = s.id
		WHERE s.execution_id = $1`, executionID)
	if err != nil {
		return nil, apperrors.Internal("listing causality edges", err)
	}
	defer edgesRows.Close()

	causalityMap := make(map[string][]string)
	for edgesRows.Next() {
		var spanID, causedByID string
		if err := edgesRows.Scan(&spanID, &causedByID); err == nil {
			causalityMap[spanID] = append(causalityMap[spanID], causedByID)
		}
	}

	for _, span := range spans {
		if edges, ok := causalityMap[span.ID]; ok {
			span.CausedBySpanIDs = edges
		}
	}

	return spans, nil
}

// GetSpan retrieves a single span by ID.
func (s *Store) GetSpan(ctx context.Context, spanID string) (*execution.Span, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, execution_id, parent_span_id, span_type, name, status,
		       started_at, ended_at, metadata, detail, error_info
		FROM spans WHERE id = $1`, spanID)

	span, err := s.scanSpanRow(row)
	if err == pgx.ErrNoRows {
		return nil, apperrors.NotFound("span", spanID)
	}
	if err != nil {
		return nil, apperrors.Internal("getting span", err)
	}
	return span, nil
}

// ─── Event Operations ───────────────────────────────────────

// GetEventsByExecution returns all events for an execution ordered by sequence.
func (s *Store) GetEventsByExecution(ctx context.Context, executionID string) ([]*execution.ExecutionEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, execution_id, span_id, event_type, payload_ref, payload_inline,
		       payload_size, content_type, timestamp, metadata, sequence_number
		FROM execution_events
		WHERE execution_id = $1
		ORDER BY sequence_number ASC`, executionID)
	if err != nil {
		return nil, apperrors.Internal("listing events", err)
	}
	defer rows.Close()

	var events []*execution.ExecutionEvent
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, evt)
	}
	return events, nil
}

// GetEventsBySpan returns all events for a specific span.
func (s *Store) GetEventsBySpan(ctx context.Context, spanID string) ([]*execution.ExecutionEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, execution_id, span_id, event_type, payload_ref, payload_inline,
		       payload_size, content_type, timestamp, metadata, sequence_number
		FROM execution_events
		WHERE span_id = $1
		ORDER BY sequence_number ASC`, spanID)
	if err != nil {
		return nil, apperrors.Internal("listing span events", err)
	}
	defer rows.Close()

	var events []*execution.ExecutionEvent
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, evt)
	}
	return events, nil
}

// ─── Internal Helpers ───────────────────────────────────────

func spanDetail(s *execution.Span) interface{} {
	switch {
	case s.ToolCall != nil:
		return s.ToolCall
	case s.ModelInvocation != nil:
		return s.ModelInvocation
	case s.StateMutation != nil:
		return s.StateMutation
	case s.HumanApproval != nil:
		return s.HumanApproval
	case s.PolicyEvaluation != nil:
		return s.PolicyEvaluation
	default:
		return map[string]interface{}{}
	}
}

func (s *Store) scanSpan(rows pgx.Rows) (*execution.Span, error) {
	var span execution.Span
	var parentSpanID *string
	var spanType, status string
	var metadata, detail, errorInfo []byte

	if err := rows.Scan(&span.ID, &span.ExecutionID, &parentSpanID, &spanType, &span.Name, &status,
		&span.StartedAt, &span.EndedAt, &metadata, &detail, &errorInfo); err != nil {
		return nil, apperrors.Internal("scanning span", err)
	}

	span.ParentSpanID = parentSpanID
	span.SpanType = execution.SpanKind(spanType)
	span.Status = execution.Status(status)

	if err := unmarshalJSON(metadata, &span.Metadata); err != nil {
		s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span metadata")
	}

	if len(detail) > 0 && string(detail) != "{}" {
		switch span.SpanType {
		case execution.SpanKindToolCall:
			span.ToolCall = &execution.ToolCallDetail{}
			if err := unmarshalJSON(detail, span.ToolCall); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span tool call detail")
			}
		case execution.SpanKindModelInvocation:
			span.ModelInvocation = &execution.ModelInvocationDetail{}
			if err := unmarshalJSON(detail, span.ModelInvocation); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span model invocation detail")
			}
		case execution.SpanKindStateMutation:
			span.StateMutation = &execution.StateMutationDetail{}
			if err := unmarshalJSON(detail, span.StateMutation); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span state mutation detail")
			}
		case execution.SpanKindHumanApproval:
			span.HumanApproval = &execution.HumanApprovalDetail{}
			if err := unmarshalJSON(detail, span.HumanApproval); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span human approval detail")
			}
		case execution.SpanKindPolicyEvaluation:
			span.PolicyEvaluation = &execution.PolicyEvaluationDetail{}
			if err := unmarshalJSON(detail, span.PolicyEvaluation); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span policy evaluation detail")
			}
		}
	}

	if errorInfo != nil {
		span.Error = &execution.SpanError{}
		if err := unmarshalJSON(errorInfo, span.Error); err != nil {
			s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span error info")
		}
	}

	return &span, nil
}

func (s *Store) scanSpanRow(row pgx.Row) (*execution.Span, error) {
	var span execution.Span
	var parentSpanID *string
	var spanType, status string
	var metadata, detail, errorInfo []byte

	if err := row.Scan(&span.ID, &span.ExecutionID, &parentSpanID, &spanType, &span.Name, &status,
		&span.StartedAt, &span.EndedAt, &metadata, &detail, &errorInfo); err != nil {
		return nil, err
	}

	span.ParentSpanID = parentSpanID
	span.SpanType = execution.SpanKind(spanType)
	span.Status = execution.Status(status)

	if err := unmarshalJSON(metadata, &span.Metadata); err != nil {
		s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span metadata")
	}

	if len(detail) > 0 && string(detail) != "{}" {
		switch span.SpanType {
		case execution.SpanKindToolCall:
			span.ToolCall = &execution.ToolCallDetail{}
			if err := unmarshalJSON(detail, span.ToolCall); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span tool call detail")
			}
		case execution.SpanKindModelInvocation:
			span.ModelInvocation = &execution.ModelInvocationDetail{}
			if err := unmarshalJSON(detail, span.ModelInvocation); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span model invocation detail")
			}
		case execution.SpanKindStateMutation:
			span.StateMutation = &execution.StateMutationDetail{}
			if err := unmarshalJSON(detail, span.StateMutation); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span state mutation detail")
			}
		case execution.SpanKindHumanApproval:
			span.HumanApproval = &execution.HumanApprovalDetail{}
			if err := unmarshalJSON(detail, span.HumanApproval); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span human approval detail")
			}
		case execution.SpanKindPolicyEvaluation:
			span.PolicyEvaluation = &execution.PolicyEvaluationDetail{}
			if err := unmarshalJSON(detail, span.PolicyEvaluation); err != nil {
				s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span policy evaluation detail")
			}
		}
	}

	if errorInfo != nil {
		span.Error = &execution.SpanError{}
		if err := unmarshalJSON(errorInfo, span.Error); err != nil {
			s.logger.Warn().Err(err).Str("span_id", span.ID).Msg("failed to unmarshal span error info")
		}
	}

	return &span, nil
}

func scanEvent(rows interface{ Scan(...any) error }) (*execution.ExecutionEvent, error) {
	var evt execution.ExecutionEvent
	var payloadRef *string // kept for schema compat, never populated
	var payloadInline []byte
	var payloadSize int64
	var contentType string
	var metadata []byte

	if err := rows.Scan(&evt.ID, &evt.ExecutionID, &evt.SpanID, &evt.EventType,
		&payloadRef, &payloadInline, &payloadSize, &contentType,
		&evt.Timestamp, &metadata, &evt.SequenceNumber); err != nil {
		return nil, apperrors.Internal("scanning event", err)
	}
	_ = payloadRef

	_ = unmarshalJSON(metadata, &evt.Metadata)

	if payloadInline != nil {
		var inline interface{}
		_ = unmarshalJSON(payloadInline, &inline)
		evt.Payload = &execution.PayloadRef{
			Inline:      inline,
			SizeBytes:   payloadSize,
			ContentType: contentType,
		}
	}

	return &evt, nil
}

func payloadSizeOrZero(p *execution.PayloadRef) int64 {
	if p == nil {
		return 0
	}
	return p.SizeBytes
}

func payloadContentType(p *execution.PayloadRef) string {
	if p == nil || p.ContentType == "" {
		return "application/json"
	}
	return p.ContentType
}
