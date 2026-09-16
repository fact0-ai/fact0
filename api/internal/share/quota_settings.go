package share

import (
	"context"

	"github.com/fact0-ai/fact0/internal/audit"
)

// GetQuotaAlertSettings implements audit.QuotaSettingsStore.
func (s *Store) GetQuotaAlertSettings(ctx context.Context, tenantID string) (audit.QuotaAlertSettings, error) {
	settings, err := s.GetSettings(ctx, tenantID)
	if err != nil {
		return audit.QuotaAlertSettings{}, err
	}
	return audit.QuotaAlertSettings{
		AlertEmail:      settings.AlertEmail,
		QuotaAlertMonth: settings.QuotaAlertMonth,
		QuotaAlertLevel: settings.QuotaAlertLevel,
	}, nil
}

// UpdateQuotaAlertSettings merges quota alert fields into tenant settings.
func (s *Store) UpdateQuotaAlertSettings(ctx context.Context, tenantID string, q audit.QuotaAlertSettings) error {
	settings, err := s.GetSettings(ctx, tenantID)
	if err != nil {
		return err
	}
	settings.AlertEmail = q.AlertEmail
	settings.QuotaAlertMonth = q.QuotaAlertMonth
	settings.QuotaAlertLevel = q.QuotaAlertLevel
	return s.UpdateSettings(ctx, tenantID, settings)
}
