package audit

import (
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// Validate enforces the canonical schema requirements on an inbound event.
// IDs, omitted timestamps and hashes are populated by the service. Explicit
// timestamps must fit the supported audit history range.
func Validate(e *AuditEvent) error {
	if e == nil {
		return apperrors.InvalidInput("event must not be nil")
	}
	if !e.Timestamp.IsZero() && e.Timestamp.Before(historyStart) {
		return apperrors.Validation("timestamp", "must be at or after 1970-01-01T00:00:00Z")
	}
	if e.Timestamp.After(farFuture) {
		return apperrors.Validation("timestamp", "must be at or before 9999-12-31T23:59:59.999999999Z")
	}
	if e.Actor.ID == "" {
		return apperrors.Validation("actor.id", "must not be empty")
	}
	if !e.Actor.Type.IsValid() {
		return apperrors.Validation("actor.type", "must be one of human|agent|system")
	}
	if e.Action == "" {
		return apperrors.Validation("action", "must not be empty")
	}
	if e.Resource.ID == "" {
		return apperrors.Validation("resource.id", "must not be empty")
	}
	if e.Resource.Type == "" {
		return apperrors.Validation("resource.type", "must not be empty")
	}
	if !e.Outcome.IsValid() {
		return apperrors.Validation("outcome", "must be one of success|failure|error")
	}
	return nil
}
