// Package outbox implements Postgres-backed async audit ingest.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// Receipt is the client-visible ingest handle.
type Receipt struct {
	ID            string             `json:"receipt_id"`
	TenantID      string             `json:"tenant_id"`
	Status        string             `json:"status"`
	AcceptedCount int                `json:"accepted"`
	RejectedCount int                `json:"rejected"`
	EventIDs      []string           `json:"ids,omitempty"`
	Errors        []audit.BatchError `json:"errors,omitempty"`
	ErrorMessage  string             `json:"error_message,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	CommittedAt   *time.Time         `json:"committed_at,omitempty"`
}

// Row is a claimed outbox message.
type Row struct {
	ID        string
	ReceiptID string
	TenantID  string
	Payload   []byte
	Attempts  int
}

// Store persists receipts and outbox rows.
type Store struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewStore constructs an outbox Store.
func NewStore(pool *pgxpool.Pool, logger zerolog.Logger) *Store {
	return &Store{
		pool:   pool,
		logger: logger.With().Str("component", "ingest_outbox").Logger(),
	}
}

// Enqueue inserts a receipt and pending outbox row atomically.
func (s *Store) Enqueue(
	ctx context.Context,
	tenantID, receiptID string,
	events []*audit.AuditEvent,
	validation *audit.BatchResult,
) error {
	payload, err := json.Marshal(events)
	if err != nil {
		return apperrors.Internal("marshaling outbox payload", err)
	}
	errorsJSON, err := json.Marshal(validation.Errors)
	if err != nil {
		return apperrors.Internal("marshaling receipt errors", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperrors.Internal("begin outbox tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO ingest_receipts (id, tenant_id, status, accepted_count, rejected_count, errors)
		VALUES ($1, $2, 'queued', $3, $4, $5)`,
		receiptID, tenantID, validation.Accepted, validation.Rejected, errorsJSON,
	)
	if err != nil {
		s.logger.Error().Err(err).
			Str("tenant_id", tenantID).
			Str("receipt_id", receiptID).
			Msg("inserting ingest receipt")
		return apperrors.Internal("inserting ingest receipt", err)
	}

	outboxID := "obx_" + receiptID
	_, err = tx.Exec(ctx, `
		INSERT INTO ingest_outbox (id, receipt_id, tenant_id, payload, status)
		VALUES ($1, $2, $3, $4, 'pending')`,
		outboxID, receiptID, tenantID, payload,
	)
	if err != nil {
		return apperrors.Internal("inserting outbox row", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return apperrors.Internal("commit outbox tx", err)
	}
	return nil
}

// GetReceipt returns a receipt scoped to tenant.
func (s *Store) GetReceipt(ctx context.Context, tenantID, receiptID string) (*Receipt, error) {
	var r Receipt
	var eventIDs []byte
	var errorsJSON []byte
	var committedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, status, accepted_count, rejected_count,
		       event_ids, errors, COALESCE(error_message, ''), created_at, committed_at
		FROM ingest_receipts
		WHERE id = $1 AND tenant_id = $2`,
		receiptID, tenantID,
	).Scan(&r.ID, &r.TenantID, &r.Status, &r.AcceptedCount, &r.RejectedCount,
		&eventIDs, &errorsJSON, &r.ErrorMessage, &r.CreatedAt, &committedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("ingest_receipt", receiptID)
	}
	if err != nil {
		return nil, apperrors.Internal("getting ingest receipt", err)
	}
	r.CommittedAt = committedAt
	if len(eventIDs) > 0 {
		_ = json.Unmarshal(eventIDs, &r.EventIDs)
	}
	if len(errorsJSON) > 0 {
		_ = json.Unmarshal(errorsJSON, &r.Errors)
	}
	return &r, nil
}

// ClaimPending claims up to limit pending rows for processing.
func (s *Store) ClaimPending(ctx context.Context, limit int) ([]Row, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apperrors.Internal("begin claim tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, receipt_id, tenant_id, payload, attempts
		FROM ingest_outbox
		WHERE status = 'pending' AND available_at <= NOW()
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, apperrors.Internal("claiming outbox rows", err)
	}
	defer rows.Close()

	var claimed []Row
	var ids []string
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.ID, &row.ReceiptID, &row.TenantID, &row.Payload, &row.Attempts); err != nil {
			return nil, apperrors.Internal("scanning outbox row", err)
		}
		claimed = append(claimed, row)
		ids = append(ids, row.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Internal("iterating outbox rows", err)
	}
	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, apperrors.Internal("commit empty claim tx", err)
		}
		return nil, nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE ingest_outbox SET status = 'processing'
		WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, apperrors.Internal("marking outbox processing", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apperrors.Internal("commit claim tx", err)
	}
	return claimed, nil
}

// MarkCommitted marks outbox row and receipt as successfully processed.
func (s *Store) MarkCommitted(ctx context.Context, outboxID, receiptID string, eventIDs []string) error {
	idsJSON, err := json.Marshal(eventIDs)
	if err != nil {
		return apperrors.Internal("marshaling event ids", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperrors.Internal("begin commit tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		UPDATE ingest_outbox
		SET status = 'done', processed_at = NOW()
		WHERE id = $1`, outboxID)
	if err != nil {
		return apperrors.Internal("marking outbox done", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE ingest_receipts
		SET status = 'committed', event_ids = $2, committed_at = NOW()
		WHERE id = $1`, receiptID, idsJSON)
	if err != nil {
		return apperrors.Internal("marking receipt committed", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return apperrors.Internal("commit outbox success tx", err)
	}
	return nil
}

// MarkRetry schedules a retry or permanent failure.
func (s *Store) MarkRetry(ctx context.Context, outboxID, receiptID string, attempts, maxAttempts int, errMsg string, backoff time.Duration) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperrors.Internal("begin retry tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if attempts >= maxAttempts {
		_, err = tx.Exec(ctx, `
			UPDATE ingest_outbox
			SET status = 'failed', processed_at = NOW(), error_message = $2, attempts = $3
			WHERE id = $1`, outboxID, errMsg, attempts)
		if err != nil {
			return apperrors.Internal("marking outbox failed", err)
		}
		_, err = tx.Exec(ctx, `
			UPDATE ingest_receipts
			SET status = 'failed', error_message = $2
			WHERE id = $1`, receiptID, errMsg)
		if err != nil {
			return apperrors.Internal("marking receipt failed", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return apperrors.Internal("commit outbox fail tx", err)
		}
		return nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE ingest_outbox
		SET status = 'pending',
		    attempts = $2,
		    available_at = $3,
		    error_message = $4
		WHERE id = $1`, outboxID, attempts, time.Now().UTC().Add(backoff), errMsg)
	if err != nil {
		return apperrors.Internal("scheduling outbox retry", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return apperrors.Internal("commit outbox retry tx", err)
	}
	return nil
}

// OldestPendingLag returns the age of the oldest pending outbox row.
func (s *Store) OldestPendingLag(ctx context.Context) (time.Duration, error) {
	var created time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT created_at FROM ingest_outbox
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT 1`).Scan(&created)
	if errors.Is(err, pgx.ErrNoRows) {
		metrics.IngestOutboxLagSeconds.Set(0)
		return 0, nil
	}
	if err != nil {
		return 0, apperrors.Internal("outbox lag query", err)
	}
	lag := time.Since(created)
	metrics.IngestOutboxLagSeconds.Set(lag.Seconds())
	return lag, nil
}

// ResetStaleProcessing resets outbox items stuck in 'processing' status for more than a given age back to 'pending'.
func (s *Store) ResetStaleProcessing(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ingest_outbox
		SET status = 'pending', available_at = NOW(), error_message = 'worker timeout / crash'
		WHERE status = 'processing' AND available_at < NOW() - $1::interval`,
		olderThan)
	if err != nil {
		return 0, apperrors.Internal("resetting stale processing outbox rows", err)
	}
	return tag.RowsAffected(), nil
}
