package copilot

import (
	"context"
	"time"
)

// Repository persists copilot sessions and messages.
type Repository interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, tenantID, userID, sessionID string) (*Session, error)
	ListSessions(ctx context.Context, tenantID, userID string, since time.Time) ([]*Session, error)
	TouchSession(ctx context.Context, tenantID, userID, sessionID string, title string) error

	AppendMessages(ctx context.Context, tenantID, sessionID string, msgs []MessageInput) error
	ListMessages(ctx context.Context, tenantID, userID, sessionID string) ([]*Message, error)

	InsertActivity(ctx context.Context, a *Activity) error

	// GetUsage returns the current message count for a user in the given period (YYYY-MM).
	GetUsage(ctx context.Context, tenantID, userID, period string) (int, error)

	// IncrementUsage atomically increments the message count and returns the new total.
	IncrementUsage(ctx context.Context, tenantID, userID, period string) (int, error)
}
