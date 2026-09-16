package outbox

import (
	"context"
	"encoding/json"

	"github.com/fact0-ai/fact0/internal/audit"
)

// Adapter implements audit.BatchEnqueuer and audit.ReceiptStore.
type Adapter struct {
	store *Store
}

// NewAdapter wraps a Store for the audit service.
func NewAdapter(store *Store) *Adapter {
	return &Adapter{store: store}
}

// Enqueue implements audit.BatchEnqueuer.
func (a *Adapter) Enqueue(ctx context.Context, tenantID, receiptID string, events []*audit.AuditEvent, validation *audit.BatchResult) error {
	return a.store.Enqueue(ctx, tenantID, receiptID, events, validation)
}

// GetReceipt implements audit.ReceiptStore.
func (a *Adapter) GetReceipt(ctx context.Context, tenantID, receiptID string) (*audit.AsyncReceipt, error) {
	r, err := a.store.GetReceipt(ctx, tenantID, receiptID)
	if err != nil {
		return nil, err
	}
	return &audit.AsyncReceipt{
		ReceiptID: r.ID,
		Status:    r.Status,
		BatchResult: audit.BatchResult{
			Accepted: r.AcceptedCount,
			Rejected: r.RejectedCount,
			IDs:      r.EventIDs,
			Errors:   r.Errors,
		},
		ErrorMessage: r.ErrorMessage,
		CreatedAt:    r.CreatedAt,
		CommittedAt:  r.CommittedAt,
	}, nil
}

// ServiceCommitter adapts audit.Service for the outbox worker.
type ServiceCommitter struct {
	Svc *audit.Service
}

// CommitBatch implements Committer.
func (c ServiceCommitter) CommitBatch(ctx context.Context, tenantID string, events []*audit.AuditEvent) ([]*audit.AuditEvent, error) {
	return c.Svc.CommitBatch(ctx, tenantID, events)
}

// Fanout implements Committer.
func (c ServiceCommitter) Fanout(events []*audit.AuditEvent) {
	c.Svc.FanoutEvents(events)
}

// DecodePayload helper for tests.
func DecodePayload(payload []byte) ([]*audit.AuditEvent, error) {
	var events []*audit.AuditEvent
	if err := json.Unmarshal(payload, &events); err != nil {
		return nil, err
	}
	return events, nil
}
