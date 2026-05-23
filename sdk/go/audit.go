package fact0

import "context"

// Actor describes who performed an action.
type Actor struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Email string `json:"email,omitempty"`
}

// Resource identifies the action target.
type Resource struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// AuditEventInput is the ingest payload for a single audit event.
type AuditEventInput struct {
	ID        string                 `json:"id,omitempty"`
	Timestamp string                 `json:"timestamp,omitempty"`
	Actor     Actor                  `json:"actor"`
	Action    string                 `json:"action"`
	Resource  Resource               `json:"resource"`
	Outcome   string                 `json:"outcome"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AuditClient wraps audit REST endpoints.
type AuditClient struct {
	parent *Client
}

// Log ingests a single event via the batch endpoint.
func (a *AuditClient) Log(ctx context.Context, event AuditEventInput) error {
	return a.parent.doJSON(ctx, "POST", "/v1/events/batch", map[string]any{
		"events": []AuditEventInput{event},
	}, nil, true)
}

// LogBatch ingests up to 1000 events.
func (a *AuditClient) LogBatch(ctx context.Context, events []AuditEventInput) (map[string]any, error) {
	var out map[string]any
	err := a.parent.doJSON(ctx, "POST", "/v1/events/batch", map[string]any{"events": events}, &out, true)
	return out, err
}

// GetEvent fetches one event by ID.
func (a *AuditClient) GetEvent(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	err := a.parent.doJSON(ctx, "GET", "/v1/events/"+id, nil, &out, true)
	return out, err
}

// ListEvents queries the audit log.
func (a *AuditClient) ListEvents(ctx context.Context, query string) (map[string]any, error) {
	var out map[string]any
	err := a.parent.doJSON(ctx, "GET", "/v1/events"+query, nil, &out, true)
	return out, err
}

// Verify walks the hash chain.
func (a *AuditClient) Verify(ctx context.Context, query string) (map[string]any, error) {
	var out map[string]any
	err := a.parent.doJSON(ctx, "GET", "/v1/verify"+query, nil, &out, true)
	return out, err
}

// GetReceipt polls an async ingest receipt.
func (a *AuditClient) GetReceipt(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	err := a.parent.doJSON(ctx, "GET", "/v1/receipts/"+id, nil, &out, true)
	return out, err
}
