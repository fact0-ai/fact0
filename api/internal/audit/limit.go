package audit

import (
	"context"
	"time"

	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// QuotaAlertSettings holds tenant fields used for deduped quota emails.
type QuotaAlertSettings struct {
	AlertEmail      string
	QuotaAlertMonth string
	QuotaAlertLevel int
}

// QuotaNotifier sends threshold emails.
type QuotaNotifier interface {
	QuotaAlert(ctx context.Context, tenantID, workspaceName string, used, limit int64, percentage int)
}

// QuotaSettingsStore reads/writes tenant alert settings for deduped quota emails.
type QuotaSettingsStore interface {
	GetQuotaAlertSettings(ctx context.Context, tenantID string) (QuotaAlertSettings, error)
	UpdateQuotaAlertSettings(ctx context.Context, tenantID string, settings QuotaAlertSettings) error
}

// MonthlyLimiter enforces per-plan monthly audit event caps before ingest.
type MonthlyLimiter struct {
	plans             PlanStore
	stats             StatsStore
	settings          QuotaSettingsStore
	notify            QuotaNotifier
	tenants           TenantStore
	OverrideFreeLimit int64
}

// NewMonthlyLimiter constructs a limiter. plans or stats may be nil (checks skipped).
func NewMonthlyLimiter(
	plans PlanStore,
	stats StatsStore,
	settings QuotaSettingsStore,
	notify QuotaNotifier,
	tenants TenantStore,
	overrideFreeLimit int64,
) *MonthlyLimiter {
	return &MonthlyLimiter{
		plans:             plans,
		stats:             stats,
		settings:          settings,
		notify:            notify,
		tenants:           tenants,
		OverrideFreeLimit: overrideFreeLimit,
	}
}

// Check returns nil when `incoming` events may be accepted this month.
func (l *MonthlyLimiter) Check(ctx context.Context, tenantID string, incoming int) error {
	if l == nil || l.plans == nil || l.stats == nil || tenantID == "" || incoming <= 0 {
		return nil
	}

	plan, err := l.plans.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return err
	}

	limit := plan.MonthlyEventLimit
	if plan.ID == "free" && l.OverrideFreeLimit > 0 {
		limit = &l.OverrideFreeLimit
	}
	if limit == nil || *limit <= 0 {
		return nil
	}

	used, err := l.stats.CountEventsThisMonth(ctx, tenantID)
	if err != nil {
		return err
	}

	l.maybeNotifyQuota(ctx, tenantID, used, *limit)

	if used+int64(incoming) > *limit {
		l.maybeNotifyQuotaExceeded(ctx, tenantID, used+int64(incoming), *limit)
		return apperrors.MonthlyLimitExceeded(*limit, used)
	}
	return nil
}

func (l *MonthlyLimiter) maybeNotifyQuota(ctx context.Context, tenantID string, used, limit int64) {
	if l == nil || l.settings == nil || l.notify == nil || limit <= 0 {
		return
	}
	pct := int(used * 100 / limit)

	// We want to trigger emails at 20,000 (66% of 30k) and 25,000 (83% of 30k)
	// and 30,000 (100% of 30k). For other limit sizes, we use the proportional percentages.
	var level int
	if used >= limit {
		level = 100
	} else if (limit == 30000 && used >= 25000) || (limit != 30000 && pct >= 83) {
		level = 83
	} else if (limit == 30000 && used >= 20000) || (limit != 30000 && pct >= 66) {
		level = 66
	} else {
		return
	}

	settings, err := l.settings.GetQuotaAlertSettings(ctx, tenantID)
	if err != nil || settings.AlertEmail == "" {
		return
	}
	month := time.Now().UTC().Format("2006-01")
	if settings.QuotaAlertMonth == month && settings.QuotaAlertLevel >= level {
		return
	}

	l.notify.QuotaAlert(ctx, tenantID, l.workspaceName(ctx, tenantID), used, limit, pct)

	settings.QuotaAlertMonth = month
	settings.QuotaAlertLevel = level
	_ = l.settings.UpdateQuotaAlertSettings(ctx, tenantID, settings)
}

func (l *MonthlyLimiter) maybeNotifyQuotaExceeded(ctx context.Context, tenantID string, used, limit int64) {
	if l == nil || l.settings == nil || l.notify == nil {
		return
	}
	settings, err := l.settings.GetQuotaAlertSettings(ctx, tenantID)
	if err != nil || settings.AlertEmail == "" {
		return
	}
	month := time.Now().UTC().Format("2006-01")
	if settings.QuotaAlertMonth == month && settings.QuotaAlertLevel >= 100 {
		return
	}
	l.notify.QuotaAlert(ctx, tenantID, l.workspaceName(ctx, tenantID), used, limit, 100)
	settings.QuotaAlertMonth = month
	settings.QuotaAlertLevel = 100
	_ = l.settings.UpdateQuotaAlertSettings(ctx, tenantID, settings)
}

func (l *MonthlyLimiter) workspaceName(ctx context.Context, tenantID string) string {
	if l.tenants != nil {
		if t, err := l.tenants.GetTenant(ctx, tenantID); err == nil && t.Name != "" {
			return t.Name
		}
	}
	return tenantID
}
