package notify

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/share"
)

// TenantSettingsLoader reads per-tenant alert configuration.
type TenantSettingsLoader interface {
	GetSettings(ctx context.Context, tenantID string) (share.TenantSettings, error)
	UpdateSettings(ctx context.Context, tenantID string, settings share.TenantSettings) error
}

// Alerter posts tenant-configured webhooks and optional alert emails.
type Alerter struct {
	settings    TenantSettingsLoader
	emailBridge *EmailBridge
	logger      zerolog.Logger
}

// NewAlerter constructs an Alerter.
func NewAlerter(settings TenantSettingsLoader, emailBridge *EmailBridge, logger zerolog.Logger) *Alerter {
	return &Alerter{
		settings:    settings,
		emailBridge: emailBridge,
		logger:      logger.With().Str("component", "alerter").Logger(),
	}
}

// ChainBreak notifies when verification finds a broken chain.
func (a *Alerter) ChainBreak(ctx context.Context, tenantID, workspaceName string, valid bool, brokenCount int, reason string) {
	if a == nil || a.settings == nil || valid {
		return
	}
	payload := ChainBreakPayload{
		Event:       "fact0.chain.verify_failed",
		TenantID:    tenantID,
		Valid:       valid,
		BrokenCount: brokenCount,
		Reason:      reason,
	}
	a.postWebhook(ctx, tenantID, payload)
	a.postChainFailedEmail(ctx, tenantID, workspaceName, reason)
}

// ReceiptFailed notifies when async ingest exhausts retries.
func (a *Alerter) ReceiptFailed(ctx context.Context, tenantID, receiptID, errMsg string) {
	if a == nil || a.settings == nil {
		return
	}
	a.postWebhook(ctx, tenantID, ChainBreakPayload{
		Event:    "fact0.ingest.receipt_failed",
		TenantID: tenantID,
		Valid:    false,
		Reason:   receiptID + ": " + errMsg,
	})
}

// QuotaAlert sends usage threshold emails (80 or 100).
func (a *Alerter) QuotaAlert(ctx context.Context, tenantID, workspaceName string, used, limit int64, percentage int) {
	if a == nil || a.settings == nil || a.emailBridge == nil {
		return
	}
	settings, err := a.settings.GetSettings(ctx, tenantID)
	if err != nil || settings.AlertEmail == "" {
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.emailBridge.Send(bg, "quota-alert", settings.AlertEmail, map[string]any{
			"workspaceName": workspaceName,
			"usageCount":    used,
			"limitCount":    limit,
			"percentage":    percentage,
		}); err != nil {
			a.logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("quota alert email failed")
		}
	}()
}

// ReportReady notifies that an evidence pack export completed.
func (a *Alerter) ReportReady(ctx context.Context, tenantID, workspaceName, reportName, downloadURL, dateRange string, eventCount int) {
	if a == nil || a.settings == nil || a.emailBridge == nil {
		return
	}
	settings, err := a.settings.GetSettings(ctx, tenantID)
	if err != nil || settings.AlertEmail == "" {
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.emailBridge.Send(bg, "report-ready", settings.AlertEmail, map[string]any{
			"reportName":    reportName,
			"workspaceName": workspaceName,
			"downloadUrl":   downloadURL,
			"eventCount":    eventCount,
			"dateRange":     dateRange,
		}); err != nil {
			a.logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("report ready email failed")
		}
	}()
}

// TestEmail sends a chain-failed template for alert configuration testing.
func (a *Alerter) TestEmail(ctx context.Context, tenantID, workspaceName, to string) error {
	if a == nil || a.emailBridge == nil {
		return nil
	}
	return a.emailBridge.Send(ctx, "chain-failed", to, map[string]any{
		"workspaceName": workspaceName,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"failedEventId": "evt_test",
		"expectedHash":  "sha256:test…",
		"actualHash":    "sha256:test…",
	})
}

func (a *Alerter) postWebhook(ctx context.Context, tenantID string, payload ChainBreakPayload) {
	settings, err := a.settings.GetSettings(ctx, tenantID)
	if err != nil || settings.AlertWebhookURL == "" {
		return
	}
	url := settings.AlertWebhookURL
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := WebhookPOST(bg, url, payload); err != nil {
			a.logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("alert webhook failed")
		}
	}()
}

func (a *Alerter) postChainFailedEmail(ctx context.Context, tenantID, workspaceName, reason string) {
	if a.emailBridge == nil {
		return
	}
	settings, err := a.settings.GetSettings(ctx, tenantID)
	if err != nil || settings.AlertEmail == "" {
		return
	}
	name := workspaceName
	if name == "" {
		name = tenantID
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.emailBridge.Send(bg, "chain-failed", settings.AlertEmail, map[string]any{
			"workspaceName": name,
			"timestamp":     time.Now().UTC().Format(time.RFC3339),
			"failedEventId": reason,
		}); err != nil {
			a.logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("chain failed email failed")
		}
	}()
}
