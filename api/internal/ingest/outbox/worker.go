package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/notify"
)

// Committer persists validated audit events to the chain.
type Committer interface {
	CommitBatch(ctx context.Context, tenantID string, events []*audit.AuditEvent) ([]*audit.AuditEvent, error)
	Fanout(events []*audit.AuditEvent)
}

// Worker drains the Postgres outbox into the audit chain.
type Worker struct {
	store       *Store
	committer   Committer
	alerter     *notify.Alerter
	logger      zerolog.Logger
	batchSize   int
	maxAttempts int
}

// WorkerConfig configures the outbox worker.
type WorkerConfig struct {
	BatchSize   int
	MaxAttempts int
}

// NewWorker constructs a Worker.
func NewWorker(store *Store, committer Committer, alerter *notify.Alerter, logger zerolog.Logger, cfg WorkerConfig) *Worker {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 10
	}
	return &Worker{
		store:       store,
		committer:   committer,
		alerter:     alerter,
		logger:      logger.With().Str("component", "ingest_worker").Logger(),
		batchSize:   cfg.BatchSize,
		maxAttempts: cfg.MaxAttempts,
	}
}

// Run polls the outbox until ctx is cancelled.
func (w *Worker) Run(ctx context.Context, pollInterval time.Duration) {
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	t := time.NewTicker(pollInterval)
	defer t.Stop()

	reclaimTicker := time.NewTicker(2 * time.Minute)
	defer reclaimTicker.Stop()

	for {
		w.processOnce(ctx)
		_, _ = w.store.OldestPendingLag(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-reclaimTicker.C:
			count, err := w.store.ResetStaleProcessing(ctx, 5*time.Minute)
			if err != nil {
				w.logger.Error().Err(err).Msg("failed to reclaim stale processing outbox items")
			} else if count > 0 {
				w.logger.Info().Int64("count", count).Msg("reclaimed stale processing outbox items")
			}
		}
	}
}

func (w *Worker) processOnce(ctx context.Context) {
	rows, err := w.store.ClaimPending(ctx, w.batchSize)
	if err != nil {
		w.logger.Warn().Err(err).Msg("claim outbox rows")
		return
	}
	for _, row := range rows {
		w.processRow(ctx, row)
	}
}

func (w *Worker) processRow(ctx context.Context, row Row) {
	var events []*audit.AuditEvent
	if err := json.Unmarshal(row.Payload, &events); err != nil {
		_ = w.store.MarkRetry(ctx, row.ID, row.ReceiptID, row.Attempts+1, w.maxAttempts, err.Error(), time.Second)
		return
	}
	written, err := w.committer.CommitBatch(ctx, row.TenantID, events)
	if err != nil {
		backoff := time.Duration(row.Attempts+1) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		nextAttempts := row.Attempts + 1
		if nextAttempts >= w.maxAttempts && w.alerter != nil {
			w.alerter.ReceiptFailed(ctx, row.TenantID, row.ReceiptID, err.Error())
		}
		_ = w.store.MarkRetry(ctx, row.ID, row.ReceiptID, nextAttempts, w.maxAttempts, err.Error(), backoff)
		return
	}
	ids := make([]string, len(written))
	for i, e := range written {
		ids[i] = e.ID
	}
	if err := w.store.MarkCommitted(ctx, row.ID, row.ReceiptID, ids); err != nil {
		w.logger.Warn().Err(err).Str("receipt_id", row.ReceiptID).Msg("mark committed")
		return
	}
	w.committer.Fanout(written)
}
