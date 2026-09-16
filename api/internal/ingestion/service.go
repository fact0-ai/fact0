// Package ingestion handles the intake and validation of execution events.
package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/analytics"
	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

// Service handles execution event ingestion.
type Service struct {
	store    *postgres.Store
	logger   zerolog.Logger
	redactOn bool
}

// NewService creates a new ingestion service. When redactionEnabled is
// true, every span/event passes through the redaction.Redact* helpers
// before reaching Postgres - see redactSpan/redactEvent below.
func NewService(store *postgres.Store, logger zerolog.Logger, redactionEnabled bool) *Service {
	return &Service{
		store:    store,
		logger:   logger.With().Str("module", "ingestion").Logger(),
		redactOn: redactionEnabled,
	}
}

// StartExecution creates a new execution trace scoped to tenantID.
func (s *Service) StartExecution(ctx context.Context, tenantID string, req StartExecutionRequest) (*execution.Execution, error) {
	now := time.Now().UTC()
	startedAt := now.Truncate(time.Microsecond)
	if req.StartedAt != nil {
		startedAt = req.StartedAt.UTC().Truncate(time.Microsecond)
	}

	idempotencyKey := req.IdempotencyKey
	idTime := now.UnixNano()
	if idempotencyKey == "" {
		idempotencyKey = now.Format(time.RFC3339Nano)
	} else {
		idTime = 0
	}

	exec := &execution.Execution{
		ID:        stableID("exec", []any{tenantID, req.AgentID, idTime, idempotencyKey}),
		AgentID:   req.AgentID,
		AgentName: req.AgentName,
		Status:    execution.StatusRunning,
		Trigger:   req.Trigger,
		StartedAt: startedAt,
		Metadata:  req.Metadata,
	}

	if exec.Metadata == nil {
		exec.Metadata = map[string]string{}
	}

	if s.redactOn {
		s.redactExecution(exec)
	}

	if err := execution.ValidateExecution(exec); err != nil {
		metrics.IngestRejected.WithLabelValues("execution", "validation").Inc()
		return nil, err
	}

	if err := s.store.InsertExecution(ctx, tenantID, exec); err != nil {
		metrics.IngestRejected.WithLabelValues("execution", "storage").Inc()
		return nil, err
	}
	// Explicit capture time is part of the idempotent request. Server-generated
	// defaults may differ on retries, but a client cannot rewrite captured time.
	if req.StartedAt != nil && !exec.StartedAt.Equal(startedAt) {
		return nil, apperrors.AlreadyExists("execution with different started_at", exec.ID)
	}

	metrics.IngestAccepted.WithLabelValues("execution").Inc()
	s.logger.Info().Str("execution_id", exec.ID).Str("agent_id", exec.AgentID).Msg("execution started")
	return exec, nil
}

// IngestSpans persists a batch of spans and auto-generates lifecycle events.
func (s *Service) IngestSpans(ctx context.Context, tenantID, executionID string, spans []*execution.Span) (int, []IngestError) {
	if _, err := s.store.GetExecution(ctx, tenantID, executionID); err != nil {
		return 0, []IngestError{{
			EntityID: executionID,
			Code:     "NOT_FOUND",
			Message:  err.Error(),
		}}
	}
	var errors []IngestError
	accepted := 0

	for _, span := range spans {
		if span == nil {
			errors = append(errors, IngestError{Code: "VALIDATION_ERROR", Message: "span must not be null"})
			continue
		}
		span.ExecutionID = executionID
		span.StartedAt = span.StartedAt.UTC().Truncate(time.Microsecond)
		if span.EndedAt != nil {
			t := span.EndedAt.UTC().Truncate(time.Microsecond)
			span.EndedAt = &t
		}
		if span.Status == "" {
			span.Status = execution.StatusRunning
			if span.EndedAt != nil {
				span.Status = execution.StatusCompleted
			}
		}
		if span.ID == "" {
			span.ID = stableID("span", []any{executionID, span.ParentSpanID, span.Name, span.SpanType, span.StartedAt})
		}

		enrichModelInvocationCost(span)

		if s.redactOn {
			s.redactSpan(span)
		}

		if err := execution.ValidateSpan(span); err != nil {
			metrics.IngestRejected.WithLabelValues("span", "validation").Inc()
			errors = append(errors, IngestError{
				EntityID: span.ID,
				Code:     "VALIDATION_ERROR",
				Message:  err.Error(),
			})
			continue
		}

		if err := s.store.InsertSpan(ctx, span); err != nil {
			metrics.IngestRejected.WithLabelValues("span", "storage").Inc()
			errors = append(errors, IngestError{
				EntityID: span.ID,
				Code:     "STORAGE_ERROR",
				Message:  err.Error(),
			})
			continue
		}

		metrics.IngestAccepted.WithLabelValues("span").Inc()
		accepted++
	}

	s.logger.Info().Str("execution_id", executionID).Int("accepted", accepted).Int("errors", len(errors)).Msg("spans ingested")
	return accepted, errors
}

// enrichModelInvocationCost fills in cost_usd from the synced pricing table
// when the client shipped token counts but no cost (e.g. the Claude Code
// collector, which has no pricing data). An explicit non-zero cost from the
// client always wins.
func enrichModelInvocationCost(span *execution.Span) {
	mi := span.ModelInvocation
	if mi == nil || mi.CostUSD != 0 {
		return
	}
	if mi.PromptTokens+mi.CompletionTokens == 0 {
		return
	}
	mi.CostUSD = analytics.EstimateCost(mi.ModelName, mi.ModelProvider, mi.PromptTokens, mi.CompletionTokens)
}

// IngestEvents persists a batch of execution events.
func (s *Service) IngestEvents(ctx context.Context, tenantID, executionID string, events []*execution.ExecutionEvent) (int, []IngestError) {
	if _, err := s.store.GetExecution(ctx, tenantID, executionID); err != nil {
		return 0, []IngestError{{
			EntityID: executionID,
			Code:     "NOT_FOUND",
			Message:  err.Error(),
		}}
	}
	var errors []IngestError
	accepted := 0

	for _, event := range events {
		if event == nil {
			errors = append(errors, IngestError{Code: "VALIDATION_ERROR", Message: "event must not be null"})
			continue
		}
		event.ExecutionID = executionID
		event.Timestamp = event.Timestamp.UTC().Truncate(time.Microsecond)
		if event.Metadata == nil {
			event.Metadata = map[string]string{}
		}
		if event.Payload != nil && event.Payload.ContentType == "" {
			event.Payload.ContentType = "application/json"
		}
		event.SequenceNumber = 0

		if event.ID == "" {
			event.ID = stableID("evt", event)
		}

		if s.redactOn {
			s.redactEvent(event)
		}

		if err := execution.ValidateEvent(event); err != nil {
			metrics.IngestRejected.WithLabelValues("event", "validation").Inc()
			errors = append(errors, IngestError{
				EntityID: event.ID,
				Code:     "VALIDATION_ERROR",
				Message:  err.Error(),
			})
			continue
		}

		if err := s.store.InsertEvent(ctx, event); err != nil {
			metrics.IngestRejected.WithLabelValues("event", "storage").Inc()
			errors = append(errors, IngestError{
				EntityID: event.ID,
				Code:     "STORAGE_ERROR",
				Message:  err.Error(),
			})
			continue
		}

		metrics.IngestAccepted.WithLabelValues("event").Inc()
		accepted++
	}

	s.logger.Info().Str("execution_id", executionID).Int("accepted", accepted).Int("errors", len(errors)).Msg("events ingested")
	return accepted, errors
}

// EndExecution marks an execution terminal using the server's current time.
func (s *Service) EndExecution(ctx context.Context, tenantID, executionID string, status execution.Status) (*execution.Execution, error) {
	return s.EndExecutionAt(ctx, tenantID, executionID, status, nil)
}

// EndExecutionAt preserves an optional captured end time, including offline delivery.
// Omitting endedAt keeps retries compatible with the original server-time API.
func (s *Service) EndExecutionAt(ctx context.Context, tenantID, executionID string, status execution.Status, endedAt *time.Time) (*execution.Execution, error) {
	if status != execution.StatusCompleted && status != execution.StatusFailed && status != execution.StatusCancelled {
		return nil, apperrors.InvalidInput("end status must be COMPLETED, FAILED, or CANCELLED")
	}
	end := time.Now().UTC().Truncate(time.Microsecond)
	if endedAt != nil {
		if endedAt.IsZero() {
			return nil, apperrors.Validation("ended_at", "must not be zero")
		}
		end = endedAt.UTC().Truncate(time.Microsecond)
	}
	exec, err := s.store.GetExecution(ctx, tenantID, executionID)
	if err != nil {
		return nil, err
	}
	if endedAt == nil && exec.Status == status && exec.EndedAt != nil {
		// Preserve legacy retries even when a capture clock was ahead of server time.
		return exec, nil
	}
	if end.Before(exec.StartedAt) {
		return nil, apperrors.Validation("ended_at", "must be at or after started_at")
	}
	if err := s.store.UpdateExecutionStatus(ctx, tenantID, executionID, status, &end); err != nil {
		return nil, err
	}

	exec, err = s.store.GetExecution(ctx, tenantID, executionID)
	if err != nil {
		return nil, err
	}
	if endedAt != nil && (exec.EndedAt == nil || !exec.EndedAt.Equal(end)) {
		return nil, apperrors.AlreadyExists("execution with different ended_at", executionID)
	}

	s.logger.Info().Str("execution_id", executionID).Str("status", string(status)).Msg("execution ended")
	return exec, nil
}

// ─── Request/Response Types ─────────────────────────────────

// StartExecutionRequest is the input for starting a new execution.
type StartExecutionRequest struct {
	AgentID        string            `json:"agent_id"`
	AgentName      string            `json:"agent_name"`
	Trigger        string            `json:"trigger"`
	Metadata       map[string]string `json:"metadata"`
	IdempotencyKey string            `json:"idempotency_key"`
	StartedAt      *time.Time        `json:"started_at,omitempty"`
}

// IngestError represents a single ingestion failure.
type IngestError struct {
	EntityID string `json:"entity_id"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// stableID is scoped to its tenant/execution and independent of batch position.
// Clients should send explicit IDs when identical named work starts at exactly
// the same timestamp; distinct timestamps produce distinct default span IDs.
func stableID(prefix string, value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%s_%x", prefix, sum[:16])
}
