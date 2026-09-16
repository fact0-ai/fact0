package execution

import (
	"fmt"

	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// ValidateExecution validates an execution for correctness.
func ValidateExecution(e *Execution) error {
	if e.AgentID == "" {
		return apperrors.Validation("agent_id", "must not be empty")
	}
	if e.StartedAt.IsZero() {
		return apperrors.Validation("started_at", "must not be zero")
	}
	if e.EndedAt != nil && e.EndedAt.Before(e.StartedAt) {
		return apperrors.Validation("ended_at", "must be after started_at")
	}
	return nil
}

// ValidateSpan validates a span for correctness.
func ValidateSpan(s *Span) error {
	if s == nil {
		return apperrors.InvalidInput("span required")
	}
	if s.Status != StatusRunning && s.Status != StatusCompleted && s.Status != StatusFailed && s.Status != StatusCancelled {
		return apperrors.Validation("status", "invalid span status")
	}
	if (s.Status == StatusRunning) != (s.EndedAt == nil) {
		return apperrors.Validation("ended_at", "required for terminal spans and absent for RUNNING spans")
	}
	if s.ExecutionID == "" {
		return apperrors.Validation("execution_id", "must not be empty")
	}
	if s.Name == "" {
		return apperrors.Validation("name", "must not be empty")
	}
	if !isValidSpanKind(s.SpanType) {
		return apperrors.Validation("span_type", fmt.Sprintf("invalid span type: %s", s.SpanType))
	}
	if s.StartedAt.IsZero() {
		return apperrors.Validation("started_at", "must not be zero")
	}
	if s.EndedAt != nil && s.EndedAt.Before(s.StartedAt) {
		return apperrors.Validation("ended_at", "must be after started_at")
	}
	return nil
}

// ValidateEvent validates an execution event.
func ValidateEvent(e *ExecutionEvent) error {
	if e.ExecutionID == "" {
		return apperrors.Validation("execution_id", "must not be empty")
	}
	if e.SpanID == "" {
		return apperrors.Validation("span_id", "must not be empty")
	}
	if e.EventType == "" {
		return apperrors.Validation("event_type", "must not be empty")
	}
	if e.Timestamp.IsZero() {
		return apperrors.Validation("timestamp", "must not be zero")
	}
	return nil
}

func isValidSpanKind(k SpanKind) bool {
	switch k {
	case SpanKindToolCall, SpanKindModelInvocation, SpanKindStateMutation,
		SpanKindHumanApproval, SpanKindPolicyEvaluation, SpanKindCustom:
		return true
	default:
		return false
	}
}
