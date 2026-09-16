package audit

import (
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// Validate enforces the canonical schema requirements on an inbound event.
// IDs, timestamps and hashes are populated by the service, so they are not
// checked here - this validator runs on the user-supplied payload only.
func Validate(e *AuditEvent) error {
	if e == nil {
		return apperrors.InvalidInput("event must not be nil")
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
