package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/copilot"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// CopilotStore is the Postgres implementation of copilot.Repository.
type CopilotStore struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewCopilotStore constructs a CopilotStore.
func NewCopilotStore(pool *pgxpool.Pool, logger zerolog.Logger) *CopilotStore {
	return &CopilotStore{
		pool:   pool,
		logger: logger.With().Str("component", "copilot_store").Logger(),
	}
}

func (s *CopilotStore) CreateSession(ctx context.Context, sess *copilot.Session) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO copilot_sessions (id, tenant_id, user_id, org_id, title, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7)`,
		sess.ID, sess.TenantID, sess.UserID, sess.OrgID, sess.Title, sess.CreatedAt, sess.UpdatedAt,
	)
	if err != nil {
		return apperrors.Internal("inserting copilot session", err)
	}
	return nil
}

func (s *CopilotStore) GetSession(ctx context.Context, tenantID, userID, sessionID string) (*copilot.Session, error) {
	var sess copilot.Session
	var title *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, org_id, title, created_at, updated_at
		FROM copilot_sessions
		WHERE id = $1 AND tenant_id = $2 AND user_id = $3`,
		sessionID, tenantID, userID,
	).Scan(&sess.ID, &sess.TenantID, &sess.UserID, &sess.OrgID, &title, &sess.CreatedAt, &sess.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("copilot_session", sessionID)
	}
	if err != nil {
		return nil, apperrors.Internal("getting copilot session", err)
	}
	if title != nil {
		sess.Title = *title
	}
	return &sess, nil
}

func (s *CopilotStore) ListSessions(ctx context.Context, tenantID, userID string, since time.Time) ([]*copilot.Session, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, org_id, title, created_at, updated_at
		FROM copilot_sessions
		WHERE tenant_id = $1 AND user_id = $2 AND updated_at >= $3
		ORDER BY updated_at DESC
		LIMIT 20`,
		tenantID, userID, since,
	)
	if err != nil {
		return nil, apperrors.Internal("listing copilot sessions", err)
	}
	defer rows.Close()

	var out []*copilot.Session
	for rows.Next() {
		var sess copilot.Session
		var title *string
		if err := rows.Scan(&sess.ID, &sess.TenantID, &sess.UserID, &sess.OrgID, &title, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, apperrors.Internal("scanning copilot session", err)
		}
		if title != nil {
			sess.Title = *title
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}

func (s *CopilotStore) TouchSession(ctx context.Context, tenantID, userID, sessionID, title string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE copilot_sessions
		SET updated_at = NOW(),
		    title = CASE WHEN title IS NULL OR title = '' THEN NULLIF($4, '') ELSE title END
		WHERE id = $1 AND tenant_id = $2 AND user_id = $3`,
		sessionID, tenantID, userID, title,
	)
	if err != nil {
		return apperrors.Internal("touching copilot session", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("copilot_session", sessionID)
	}
	return nil
}

func (s *CopilotStore) AppendMessages(ctx context.Context, tenantID, sessionID string, msgs []copilot.MessageInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperrors.Internal("begin tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, m := range msgs {
		// pgx encodes []byte as bytea; json.RawMessage is required for JSONB columns.
		parts := m.Parts
		if len(parts) == 0 {
			parts = nil
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO copilot_messages (id, session_id, tenant_id, role, content, parts)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			m.ID, sessionID, tenantID, m.Role, m.Content, parts,
		)
		if err != nil {
			s.logger.Warn().Err(err).Str("message_id", m.ID).Str("session_id", sessionID).Msg("inserting copilot message")
			return apperrors.Internal("inserting copilot message", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return apperrors.Internal("commit tx", err)
	}
	return nil
}

func (s *CopilotStore) ListMessages(ctx context.Context, tenantID, userID, sessionID string) ([]*copilot.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.session_id, m.tenant_id, m.role, m.content, m.parts, m.redacted, m.created_at
		FROM copilot_messages m
		JOIN copilot_sessions s ON s.id = m.session_id
		WHERE m.session_id = $1 AND m.tenant_id = $2 AND s.user_id = $3
		ORDER BY m.created_at ASC`,
		sessionID, tenantID, userID,
	)
	if err != nil {
		return nil, apperrors.Internal("listing copilot messages", err)
	}
	defer rows.Close()

	var out []*copilot.Message
	for rows.Next() {
		var m copilot.Message
		var parts []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.TenantID, &m.Role, &m.Content, &parts, &m.Redacted, &m.CreatedAt); err != nil {
			return nil, apperrors.Internal("scanning copilot message", err)
		}
		if len(parts) > 0 {
			m.Parts = parts
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *CopilotStore) InsertActivity(ctx context.Context, a *copilot.Activity) error {
	var meta json.RawMessage
	if len(a.Metadata) > 0 {
		b, err := json.Marshal(a.Metadata)
		if err != nil {
			return apperrors.Internal("marshaling copilot activity metadata", err)
		}
		meta = b
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO copilot_activity (id, tenant_id, session_id, user_id, action, actor_type, outcome, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		a.ID, a.TenantID, a.SessionID, a.UserID, a.Action, a.ActorType, a.Outcome, meta, a.CreatedAt,
	)
	if err != nil {
		s.logger.Warn().Err(err).Str("activity_id", a.ID).Str("action", a.Action).Str("session_id", a.SessionID).Msg("inserting copilot activity")
		return apperrors.Internal("inserting copilot activity", err)
	}
	return nil
}

func (s *CopilotStore) GetUsage(ctx context.Context, tenantID, userID, period string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(message_count, 0)
		FROM copilot_usage
		WHERE tenant_id = $1 AND user_id = $2 AND period = $3`,
		tenantID, userID, period,
	).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, apperrors.Internal("getting copilot usage", err)
	}
	return count, nil
}

func (s *CopilotStore) IncrementUsage(ctx context.Context, tenantID, userID, period string) (int, error) {
	var newCount int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO copilot_usage (tenant_id, user_id, period, message_count, updated_at)
		VALUES ($1, $2, $3, 1, NOW())
		ON CONFLICT (tenant_id, user_id, period)
		DO UPDATE SET message_count = copilot_usage.message_count + 1, updated_at = NOW()
		RETURNING message_count`,
		tenantID, userID, period,
	).Scan(&newCount)
	if err != nil {
		return 0, apperrors.Internal("incrementing copilot usage", err)
	}
	return newCount, nil
}
