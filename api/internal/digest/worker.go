// Package digest sends the weekly "your AI coding week" summary to tenants
// with recent Claude Code activity, via the alert channels they already
// configured (email and/or webhook). Follows the retention.Sweeper shape:
// a ticker goroutine started from main, safe across multiple API replicas
// through an atomic claim on tenants.cc_digest_last_sent.
package digest

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/notify"
)

// Config tunes the worker cadence.
type Config struct {
	Interval    time.Duration // tick cadence (default 1h)
	Period      time.Duration // lookback window (default 7d)
	MinInterval time.Duration // minimum gap between digests (default 6.5d)
}

func (c *Config) defaults() {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.Period <= 0 {
		c.Period = 7 * 24 * time.Hour
	}
	if c.MinInterval <= 0 {
		c.MinInterval = 156 * time.Hour // 6.5 days
	}
}

// SummaryStore is satisfied by *postgres.AuditStore.
type SummaryStore interface {
	GetClaudeCodeWeeklySummary(ctx context.Context, tenantID string, from, to time.Time) (*audit.ClaudeCodeWeeklySummary, error)
}

// EmailSender is satisfied by *notify.EmailBridge (whose Send is nil-safe).
type EmailSender interface {
	Send(ctx context.Context, template, to string, props map[string]any) error
}

// candidate is one tenant due a digest.
type candidate struct {
	TenantID   string
	TenantName string
	AlertEmail string
	WebhookURL string
}

// Worker computes and delivers weekly digests. candidatesFn/claimFn are
// seams over the SQL so runOnce is unit-testable without Postgres.
type Worker struct {
	pool         *pgxpool.Pool
	store        SummaryStore
	email        EmailSender
	webhookPost  func(ctx context.Context, url string, payload any) error
	candidatesFn func(ctx context.Context) ([]candidate, error)
	claimFn      func(ctx context.Context, tenantID string, cutoff time.Time) (bool, error)
	cfg          Config
	logger       zerolog.Logger
	now          func() time.Time
}

// NewWorker constructs the digest worker.
func NewWorker(pool *pgxpool.Pool, store SummaryStore, email EmailSender, cfg Config, logger zerolog.Logger) *Worker {
	cfg.defaults()
	w := &Worker{
		pool:        pool,
		store:       store,
		email:       email,
		webhookPost: notify.WebhookPOST,
		cfg:         cfg,
		logger:      logger.With().Str("component", "cc_digest").Logger(),
		now:         time.Now,
	}
	w.candidatesFn = w.listCandidates
	w.claimFn = w.claim
	return w
}

// Start runs an immediate pass then ticks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info().Dur("interval", w.cfg.Interval).Msg("coding agents digest worker started")
	w.runOnce(ctx)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("coding agents digest worker stopped")
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

// listCandidates selects tenants that (a) have claude_code activity within
// the period, (b) have an alert channel configured, and (c) haven't received
// a digest within MinInterval. Opt-out is implicit: no channel, no digest.
func (w *Worker) listCandidates(ctx context.Context) ([]candidate, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT t.id, COALESCE(t.name, ''),
		       COALESCE(t.settings->>'alert_email', ''),
		       COALESCE(t.settings->>'alert_webhook_url', '')
		FROM tenants t
		WHERE t.deleted_at IS NULL
		  AND (t.cc_digest_last_sent IS NULL OR t.cc_digest_last_sent < $1)
		  AND (COALESCE(t.settings->>'alert_email','') <> '' OR COALESCE(t.settings->>'alert_webhook_url','') <> '')
		  AND EXISTS (
		      SELECT 1 FROM audit_events ae
		      WHERE ae.tenant_id = t.id AND ae.action LIKE 'claude_code.%'
		        AND ae.timestamp >= $2)`,
		w.now().Add(-w.cfg.MinInterval), w.now().Add(-w.cfg.Period))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.TenantID, &c.TenantName, &c.AlertEmail, &c.WebhookURL); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// claim atomically marks the tenant as digested-now; only one replica wins.
// Claim-before-send: a delivery failure skips this week rather than
// double-sending — the right trade for a digest.
func (w *Worker) claim(ctx context.Context, tenantID string, cutoff time.Time) (bool, error) {
	tag, err := w.pool.Exec(ctx, `
		UPDATE tenants SET cc_digest_last_sent = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		  AND (cc_digest_last_sent IS NULL OR cc_digest_last_sent < $2)`,
		tenantID, cutoff)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (w *Worker) runOnce(ctx context.Context) {
	candidates, err := w.candidatesFn(ctx)
	if err != nil {
		w.logger.Error().Err(err).Msg("digest candidate query failed")
		return
	}
	if len(candidates) == 0 {
		return
	}
	cutoff := w.now().Add(-w.cfg.MinInterval)
	sent := 0
	for _, c := range candidates {
		if ctx.Err() != nil {
			return
		}
		if err := w.processTenant(ctx, c, cutoff); err != nil {
			w.logger.Error().Err(err).Str("tenant", c.TenantID).Msg("digest delivery failed")
			continue
		}
		sent++
	}
	if sent > 0 {
		w.logger.Info().Int("sent", sent).Msg("weekly coding agents digests delivered")
	}
}

func (w *Worker) processTenant(ctx context.Context, c candidate, cutoff time.Time) error {
	ok, err := w.claimFn(ctx, c.TenantID, cutoff)
	if err != nil || !ok {
		return err // !ok: another replica claimed it — not an error
	}

	to := w.now()
	from := to.Add(-w.cfg.Period)
	summary, err := w.store.GetClaudeCodeWeeklySummary(ctx, c.TenantID, from, to)
	if err != nil {
		return err
	}
	if summary.Sessions == 0 {
		// Defensive: the EXISTS filter should prevent this.
		return nil
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if c.AlertEmail != "" && w.email != nil {
		if err := w.email.Send(sendCtx, "cc-weekly-digest", c.AlertEmail, emailProps(c, from, to, summary)); err != nil {
			w.logger.Warn().Err(err).Str("tenant", c.TenantID).Msg("digest email failed")
		}
	}
	if c.WebhookURL != "" {
		if err := w.webhookPost(sendCtx, c.WebhookURL, webhookPayload(c, from, to, summary)); err != nil {
			w.logger.Warn().Err(err).Str("tenant", c.TenantID).Msg("digest webhook failed")
		}
	}
	return nil
}
