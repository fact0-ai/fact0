package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// TransparencyConfig configures the external transparency log worker.
type TransparencyConfig struct {
	Enabled      bool          `env:"TRANSPARENCY_LOG_ENABLED, default=false"`
	WebhookURL   string        `env:"TRANSPARENCY_LOG_WEBHOOK_URL"`
	PollInterval time.Duration `env:"TRANSPARENCY_LOG_POLL_INTERVAL, default=5m"`
}

// TransparencyWorker periodically publishes chain heads to an external transparency log.
type TransparencyWorker struct {
	pool   *pgxpool.Pool
	stats  StatsStore
	cfg    TransparencyConfig
	logger zerolog.Logger
	client *http.Client
}

// NewTransparencyWorker creates a TransparencyWorker.
func NewTransparencyWorker(pool *pgxpool.Pool, stats StatsStore, cfg TransparencyConfig, logger zerolog.Logger) *TransparencyWorker {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Minute
	}
	return &TransparencyWorker{
		pool:   pool,
		stats:  stats,
		cfg:    cfg,
		logger: logger.With().Str("component", "transparency_worker").Logger(),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start begins the polling loop until ctx is cancelled.
func (w *TransparencyWorker) Start(ctx context.Context) {
	if !w.cfg.Enabled || w.cfg.WebhookURL == "" {
		w.logger.Info().Msg("transparency log worker disabled or missing webhook url")
		return
	}

	w.logger.Info().Dur("interval", w.cfg.PollInterval).Msg("transparency log worker started")

	t := time.NewTicker(w.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("transparency log worker stopped")
			return
		case <-t.C:
			w.runOnce(ctx)
		}
	}
}

func (w *TransparencyWorker) runOnce(ctx context.Context) {
	rows, err := w.pool.Query(ctx, "SELECT id FROM tenants WHERE deleted_at IS NULL")
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to fetch active tenants for transparency log")
		return
	}

	var tenantIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			tenantIDs = append(tenantIDs, id)
		}
	}
	rows.Close()

	var success, failures int
	for _, id := range tenantIDs {
		if ctx.Err() != nil {
			return
		}

		seq, hash, err := w.stats.ChainHead(ctx, id)
		if err != nil {
			w.logger.Warn().Err(err).Str("tenant_id", id).Msg("failed to get chain head")
			failures++
			continue
		}

		// If chain is empty, nothing to publish
		if seq == 0 || hash == "" {
			continue
		}

		payload := map[string]any{
			"tenant_id": id,
			"sequence":  seq,
			"hash":      hash,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.WebhookURL, bytes.NewReader(body))
		if err != nil {
			failures++
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := w.client.Do(req)
		if err != nil {
			w.logger.Warn().Err(err).Str("tenant_id", id).Msg("failed to publish chain head")
			failures++
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			success++
		} else {
			failures++
		}
	}

	if success > 0 || failures > 0 {
		w.logger.Info().Int("success", success).Int("failures", failures).Msg("transparency log sync complete")
	}
}
