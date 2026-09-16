// Package retention enforces per-plan data retention by deleting (or
// tombstoning, for the audit chain) rows older than the plan's
// retention window. Runs as a goroutine inside the main API process;
// see Start(ctx) below.
//
// Behaviour per table:
//
//   - executions / spans / execution_events: physically DELETE.
//     Telemetry has no tenant_id today, so the sweep is GLOBAL with the
//     default plan's window (typically "free", 30 days). Order matters
//     to respect FKs: events → spans → executions.
//
//   - audit_events: per-tenant lookup of plan.retention_days, then
//     TOMBSTONE rows older than the cutoff by NULLing PII columns
//     (actor_email, resource_name) and merging `redacted=true` into
//     metadata. The chain hash recipe (audit/hash.go) doesn't include
//     these columns so verification keeps passing. Rows are NEVER
//     physically deleted from audit_events - that would break the chain
//     and the compliance story.
//
// Sweeps are deliberately bounded in size (BatchSize) so a backlog
// never produces a lock-the-table query.
package retention

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/notify"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// Config controls how often the sweeper runs and how aggressive each
// pass is. All fields have sensible defaults if zero.
type Config struct {
	// Interval between full sweeps. Default 1h.
	Interval time.Duration
	// BatchSize is the maximum number of rows touched in any one
	// DELETE/UPDATE statement. Default 5000.
	BatchSize int
	// DefaultRetentionDays is the window applied to telemetry tables
	// (which have no tenant column). Falls back to the `free` plan's
	// retention_days; default 30 if that lookup also fails.
	DefaultRetentionDays int
	// CopilotSessionTTLDays is the resume window after which inactive
	// copilot sessions may be deleted once transcripts are tombstoned.
	CopilotSessionTTLDays int
}

func (c *Config) withDefaults() {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 5000
	}
	if c.DefaultRetentionDays <= 0 {
		c.DefaultRetentionDays = 30
	}
	if c.CopilotSessionTTLDays <= 0 {
		c.CopilotSessionTTLDays = 7
	}
}

// Sweeper deletes/tombstones rows older than each tenant's plan
// retention window. Safe for concurrent use; only one sweep runs at a
// time (subsequent ticks short-circuit if a prior pass is still going).
type Sweeper struct {
	pool        *pgxpool.Pool
	cfg         Config
	emailBridge *notify.EmailBridge
	logger      zerolog.Logger
}

// NewSweeper constructs a Sweeper. Call Start(ctx) to begin the loop.
func NewSweeper(pool *pgxpool.Pool, cfg Config, emailBridge *notify.EmailBridge, logger zerolog.Logger) *Sweeper {
	cfg.withDefaults()
	return &Sweeper{
		pool:        pool,
		cfg:         cfg,
		emailBridge: emailBridge,
		logger:      logger.With().Str("component", "retention").Logger(),
	}
}

// Start runs the periodic sweeper until ctx is cancelled. Runs an
// immediate sweep on entry so first-boot enforcement doesn't wait an
// hour. Blocks until ctx.Done().
func (s *Sweeper) Start(ctx context.Context) {
	s.logger.Info().
		Dur("interval", s.cfg.Interval).
		Int("batch_size", s.cfg.BatchSize).
		Int("default_retention_days", s.cfg.DefaultRetentionDays).
		Msg("retention sweeper started")

	// First pass on start so the operator sees something happen in
	// logs/metrics within seconds of deploy.
	s.runOnce(ctx)

	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("retention sweeper stopped")
			return
		case <-t.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Sweeper) runOnce(ctx context.Context) {
	start := time.Now()
	outcome := "ok"
	defer func() {
		metrics.RetentionSweepDuration.WithLabelValues(outcome).Observe(time.Since(start).Seconds())
	}()

	if err := s.sweepTelemetry(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			outcome = "cancelled"
			return
		}
		outcome = "telemetry_error"
		s.logger.Error().Err(err).Msg("telemetry sweep failed")
	}

	if err := s.sweepAudit(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			outcome = "cancelled"
			return
		}
		outcome = "audit_error"
		s.logger.Error().Err(err).Msg("audit sweep failed")
	}

	if err := s.sweepCopilot(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			outcome = "cancelled"
			return
		}
		outcome = "copilot_error"
		s.logger.Error().Err(err).Msg("copilot sweep failed")
	}

	if err := s.sendTrialRemindersAndExpirationEmails(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			outcome = "cancelled"
			return
		}
		outcome = "trial_emails_error"
		s.logger.Error().Err(err).Msg("trial reminder/expiration emails sweep failed")
	}

	s.logger.Info().Dur("duration", time.Since(start)).Str("outcome", outcome).Msg("retention sweep complete")
}

// sweepTelemetry deletes old execution_events → spans → executions.
// Telemetry has no tenant column, so we use one global cutoff. The
// cutoff is whatever the `free` plan defines; if no `free` plan exists
// (shouldn't happen after migration 007), we fall back to DefaultRetentionDays.
func (s *Sweeper) sweepTelemetry(ctx context.Context) error {
	days, err := s.defaultRetentionDays(ctx)
	if err != nil {
		return err
	}
	if days <= 0 {
		// Unlimited - nothing to do.
		return nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	s.logger.Debug().Time("cutoff", cutoff).Int("retention_days", days).Msg("telemetry sweep")

	// Order matters: child rows must go first so FK constraints don't
	// block parent deletes. execution_events references spans+executions,
	// spans references executions+spans.
	steps := []struct {
		label string
		query string
	}{
		{
			label: "execution_events",
			query: `
				DELETE FROM execution_events
				WHERE id IN (
				    SELECT id FROM execution_events
				    WHERE timestamp < $1
				    LIMIT $2
				)`,
		},
		{
			label: "spans",
			// Only delete spans whose execution is itself stale AND whose
			// span row has no children referencing it. Children get
			// removed in earlier passes (recursive on parent_span_id).
			query: `
				DELETE FROM spans
				WHERE id IN (
				    SELECT s.id FROM spans s
				    LEFT JOIN spans c ON c.parent_span_id = s.id
				    WHERE s.started_at < $1 AND c.id IS NULL
				    LIMIT $2
				)`,
		},
		{
			label: "span_causality",
			// Orphan rows whose span_id or caused_by_id no longer exist.
			// Always-safe cleanup, no time filter.
			query: `
				DELETE FROM span_causality
				WHERE NOT EXISTS (SELECT 1 FROM spans s WHERE s.id = span_causality.span_id)
				   OR NOT EXISTS (SELECT 1 FROM spans s WHERE s.id = span_causality.caused_by_id)`,
		},
		{
			label: "executions",
			query: `
				DELETE FROM executions
				WHERE id IN (
				    SELECT e.id FROM executions e
				    LEFT JOIN spans s ON s.execution_id = e.id
				    WHERE e.started_at < $1 AND s.id IS NULL
				    LIMIT $2
				)`,
		},
	}

	for _, step := range steps {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			var ct int64
			var err error
			if step.label == "span_causality" {
				// no params for the orphan cleanup
				tag, e := s.pool.Exec(ctx, step.query)
				err, ct = e, tag.RowsAffected()
			} else {
				tag, e := s.pool.Exec(ctx, step.query, cutoff, s.cfg.BatchSize)
				err, ct = e, tag.RowsAffected()
			}
			if err != nil {
				return err
			}
			if ct > 0 {
				metrics.RetentionDeleted.WithLabelValues(step.label, "").Add(float64(ct))
				s.logger.Info().Str("table", step.label).Int64("deleted", ct).Msg("retention delete")
			}
			if ct < int64(s.cfg.BatchSize) {
				break // drained
			}
		}
	}
	return nil
}

// sweepAudit walks every tenant and tombstones audit_events older than
// the tenant's plan retention window. The chain stays verifiable
// because the hash recipe doesn't include the scrubbed columns.
func (s *Sweeper) sweepAudit(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, p.retention_days
		FROM tenants t
		JOIN plans p ON p.id = t.plan_id
		WHERE t.deleted_at IS NULL AND p.retention_days IS NOT NULL`)
	if err != nil {
		return err
	}
	type tenantPlan struct {
		ID            string
		RetentionDays int
	}
	var plans []tenantPlan
	for rows.Next() {
		var tp tenantPlan
		if err := rows.Scan(&tp.ID, &tp.RetentionDays); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, tp)
	}
	rows.Close()

	for _, tp := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		cutoff := time.Now().UTC().Add(-time.Duration(tp.RetentionDays) * 24 * time.Hour)
		// Tombstone in batches. We update rows that:
		//   - Are older than cutoff, AND
		//   - Haven't already been tombstoned (metadata->>'redacted' IS NOT 'true').
		// Setting `actor_email = ''` and `resource_name = ''` is safer than
		// NULL because the schema declares NOT NULL on both columns.
		query := `
			UPDATE audit_events
			SET actor_email   = '',
			    resource_name = '',
			    metadata      = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{redacted}', 'true'::jsonb, true)
			WHERE id IN (
			    SELECT id FROM audit_events
			    WHERE tenant_id = $1
			      AND timestamp < $2
			      AND COALESCE(metadata->>'redacted', '') <> 'true'
			    LIMIT $3
			)`
		for {
			tag, err := s.pool.Exec(ctx, query, tp.ID, cutoff, s.cfg.BatchSize)
			if err != nil {
				return err
			}
			ct := tag.RowsAffected()
			if ct > 0 {
				metrics.RetentionDeleted.WithLabelValues("audit_events", tp.ID).Add(float64(ct))
				s.logger.Info().
					Str("tenant_id", tp.ID).
					Int64("tombstoned", ct).
					Int("retention_days", tp.RetentionDays).
					Msg("retention tombstone")
			}
			if ct < int64(s.cfg.BatchSize) {
				break
			}
		}
	}
	return nil
}

// defaultRetentionDays returns the retention window for the `free`
// plan, or DefaultRetentionDays from config if the plan is missing.
// Returns 0 to signal "unlimited" (skip sweep).
func (s *Sweeper) defaultRetentionDays(ctx context.Context) (int, error) {
	var days *int
	err := s.pool.QueryRow(ctx, `SELECT retention_days FROM plans WHERE id = 'free'`).Scan(&days)
	if err != nil {
		// Plan table not migrated yet - log once and fall back.
		s.logger.Warn().Err(err).Msg("plans table missing; using configured default")
		return s.cfg.DefaultRetentionDays, nil
	}
	if days == nil {
		return 0, nil // unlimited
	}
	return *days, nil
}

// sweepCopilot tombstones old copilot transcripts per plan, then deletes
// stale sessions whose messages are all redacted (or absent).
func (s *Sweeper) sweepCopilot(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id,
		       COALESCE(p.copilot_transcript_retention_days, p.retention_days) AS transcript_days
		FROM tenants t
		JOIN plans p ON p.id = t.plan_id
		WHERE t.deleted_at IS NULL`)
	if err != nil {
		return err
	}
	type tenantPlan struct {
		ID             string
		TranscriptDays *int
	}
	var plans []tenantPlan
	for rows.Next() {
		var tp tenantPlan
		if err := rows.Scan(&tp.ID, &tp.TranscriptDays); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, tp)
	}
	rows.Close()

	for _, tp := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		if tp.TranscriptDays == nil {
			continue // unlimited transcript retention
		}
		cutoff := time.Now().UTC().Add(-time.Duration(*tp.TranscriptDays) * 24 * time.Hour)
		query := `
			UPDATE copilot_messages
			SET content = '',
			    parts = NULL,
			    redacted = true
			WHERE id IN (
			    SELECT id FROM copilot_messages
			    WHERE tenant_id = $1
			      AND created_at < $2
			      AND redacted = false
			    LIMIT $3
			)`
		for {
			tag, err := s.pool.Exec(ctx, query, tp.ID, cutoff, s.cfg.BatchSize)
			if err != nil {
				return err
			}
			ct := tag.RowsAffected()
			if ct > 0 {
				metrics.RetentionDeleted.WithLabelValues("copilot_messages", tp.ID).Add(float64(ct))
				s.logger.Info().
					Str("tenant_id", tp.ID).
					Int64("tombstoned", ct).
					Int("retention_days", *tp.TranscriptDays).
					Msg("copilot transcript tombstone")
			}
			if ct < int64(s.cfg.BatchSize) {
				break
			}
		}

		activityDelete := `
			DELETE FROM copilot_activity
			WHERE id IN (
			    SELECT id FROM copilot_activity
			    WHERE tenant_id = $1 AND created_at < $2
			    LIMIT $3
			)`
		for {
			tag, err := s.pool.Exec(ctx, activityDelete, tp.ID, cutoff, s.cfg.BatchSize)
			if err != nil {
				return err
			}
			ct := tag.RowsAffected()
			if ct > 0 {
				metrics.RetentionDeleted.WithLabelValues("copilot_activity", tp.ID).Add(float64(ct))
				s.logger.Info().
					Str("tenant_id", tp.ID).
					Int64("deleted", ct).
					Msg("copilot activity cleanup")
			}
			if ct < int64(s.cfg.BatchSize) {
				break
			}
		}
	}

	sessionCutoff := time.Now().UTC().Add(-time.Duration(s.cfg.CopilotSessionTTLDays) * 24 * time.Hour)
	deleteQuery := `
		DELETE FROM copilot_sessions
		WHERE id IN (
		    SELECT cs.id
		    FROM copilot_sessions cs
		    WHERE cs.updated_at < $1
		      AND NOT EXISTS (
		          SELECT 1 FROM copilot_messages cm
		          WHERE cm.session_id = cs.id AND cm.redacted = false
		      )
		    LIMIT $2
		)`
	for {
		tag, err := s.pool.Exec(ctx, deleteQuery, sessionCutoff, s.cfg.BatchSize)
		if err != nil {
			return err
		}
		ct := tag.RowsAffected()
		if ct > 0 {
			metrics.RetentionDeleted.WithLabelValues("copilot_sessions", "").Add(float64(ct))
			s.logger.Info().Int64("deleted", ct).Msg("copilot session cleanup")
		}
		if ct < int64(s.cfg.BatchSize) {
			break
		}
	}
	return nil
}

func (s *Sweeper) sendTrialRemindersAndExpirationEmails(ctx context.Context) error {
	if s.emailBridge == nil {
		s.logger.Debug().Msg("email bridge not configured, skipping trial emails")
		return nil
	}

	// 1. Check for trials ending soon (<= 3 days remaining)
	reminderRows, err := s.pool.Query(ctx, `
		SELECT
			t.id,
			COALESCE(u.email, ''),
			COALESCE(u.name, ''),
			COALESCE(t.name, ''),
			t.subscription_current_period_end
		FROM tenants t
		LEFT JOIN tenant_external_identities te ON te.tenant_id = t.id AND te.provider = 'better_auth_org'
		LEFT JOIN "member" m ON m."organizationId" = te.external_id AND m.role = 'owner'
		LEFT JOIN "user" u ON u.id = m."userId"
		WHERE t.plan_id = 'pro'
		  AND t.subscription_current_period_end IS NOT NULL
		  AND t.subscription_current_period_end <= NOW() + INTERVAL '3 days'
		  AND t.subscription_current_period_end > NOW()
		  AND t.trial_reminder_sent = false
		  AND t.deleted_at IS NULL
	`)
	if err != nil {
		return err
	}
	defer reminderRows.Close()

	type reminderInfo struct {
		TenantID         string
		OwnerEmail       string
		OwnerName        string
		TenantName       string
		CurrentPeriodEnd time.Time
	}
	var reminders []reminderInfo
	for reminderRows.Next() {
		var r reminderInfo
		if err := reminderRows.Scan(&r.TenantID, &r.OwnerEmail, &r.OwnerName, &r.TenantName, &r.CurrentPeriodEnd); err != nil {
			return err
		}
		reminders = append(reminders, r)
	}
	reminderRows.Close()

	for _, r := range reminders {
		expiresStr := r.CurrentPeriodEnd.Format("January 2, 2006")

		props := map[string]any{
			"userName":      r.OwnerName,
			"workspaceName": r.TenantName,
			"expiresAt":     expiresStr,
		}

		// Send email to owner if email exists
		if r.OwnerEmail != "" {
			s.logger.Info().Str("to", r.OwnerEmail).Str("tenant_id", r.TenantID).Msg("sending trial reminder email to owner")
			if err := s.emailBridge.Send(ctx, "promo-reminder", r.OwnerEmail, props); err != nil {
				s.logger.Error().Err(err).Str("to", r.OwnerEmail).Msg("failed to send trial reminder email to owner")
			}
		}

		// Also send dummy/reminder email to yash@fact0.io
		s.logger.Info().Str("tenant_id", r.TenantID).Msg("sending trial reminder email to founder yash@fact0.io")
		if err := s.emailBridge.Send(ctx, "promo-reminder", "yash@fact0.io", props); err != nil {
			s.logger.Error().Err(err).Msg("failed to send trial reminder email to founder")
		}

		// Mark as sent
		_, err = s.pool.Exec(ctx, `
			UPDATE tenants
			SET trial_reminder_sent = true
			WHERE id = $1
		`, r.TenantID)
		if err != nil {
			s.logger.Error().Err(err).Str("tenant_id", r.TenantID).Msg("failed to update trial_reminder_sent")
		}
	}

	// 2. Check for trials that have expired (period end <= NOW)
	expiredRows, err := s.pool.Query(ctx, `
		SELECT
			t.id,
			COALESCE(u.email, ''),
			COALESCE(u.name, ''),
			COALESCE(t.name, ''),
			t.plan_id
		FROM tenants t
		LEFT JOIN tenant_external_identities te ON te.tenant_id = t.id AND te.provider = 'better_auth_org'
		LEFT JOIN "member" m ON m."organizationId" = te.external_id AND m.role = 'owner'
		LEFT JOIN "user" u ON u.id = m."userId"
		WHERE t.subscription_current_period_end IS NOT NULL
		  AND t.subscription_current_period_end <= NOW()
		  AND t.trial_expired_email_sent = false
		  AND t.deleted_at IS NULL
	`)
	if err != nil {
		return err
	}
	defer expiredRows.Close()

	type expiredInfo struct {
		TenantID   string
		OwnerEmail string
		OwnerName  string
		TenantName string
		PlanID     string
	}
	var expiredList []expiredInfo
	for expiredRows.Next() {
		var e expiredInfo
		if err := expiredRows.Scan(&e.TenantID, &e.OwnerEmail, &e.OwnerName, &e.TenantName, &e.PlanID); err != nil {
			return err
		}
		expiredList = append(expiredList, e)
	}
	expiredRows.Close()

	for _, e := range expiredList {
		// Only downgrade if the tenant is still on the "pro" plan (hasn't been downgraded or upgraded since)
		if e.PlanID == "pro" {
			s.logger.Info().Str("tenant_id", e.TenantID).Msg("downgrading expired trial tenant to free")
			_, err = s.pool.Exec(ctx, `
				UPDATE tenants
				SET plan_id = 'free',
				    subscription_status = 'expired'
				WHERE id = $1
			`, e.TenantID)
			if err != nil {
				s.logger.Error().Err(err).Str("tenant_id", e.TenantID).Msg("failed to downgrade expired trial tenant")
				continue
			}
		}

		props := map[string]any{
			"userName":      e.OwnerName,
			"workspaceName": e.TenantName,
		}

		// Send email to owner if email exists
		if e.OwnerEmail != "" {
			s.logger.Info().Str("to", e.OwnerEmail).Str("tenant_id", e.TenantID).Msg("sending trial expired email to owner")
			if err := s.emailBridge.Send(ctx, "promo-expired", e.OwnerEmail, props); err != nil {
				s.logger.Error().Err(err).Str("to", e.OwnerEmail).Msg("failed to send trial expired email to owner")
			}
		}

		// Also send dummy/reminder email to yash@fact0.io
		s.logger.Info().Str("tenant_id", e.TenantID).Msg("sending trial expired email to founder yash@fact0.io")
		if err := s.emailBridge.Send(ctx, "promo-expired", "yash@fact0.io", props); err != nil {
			s.logger.Error().Err(err).Msg("failed to send trial expired email to founder")
		}

		// Mark as sent
		_, err = s.pool.Exec(ctx, `
			UPDATE tenants
			SET trial_expired_email_sent = true
			WHERE id = $1
		`, e.TenantID)
		if err != nil {
			s.logger.Error().Err(err).Str("tenant_id", e.TenantID).Msg("failed to update trial_expired_email_sent")
		}
	}

	return nil
}
