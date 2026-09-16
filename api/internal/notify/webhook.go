package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookPOST sends a JSON payload to url with a short timeout.
func WebhookPOST(ctx context.Context, url string, payload any) error {
	if url == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

// ChainBreakPayload is sent when audit chain verification fails.
type ChainBreakPayload struct {
	Event       string `json:"event"`
	TenantID    string `json:"tenant_id"`
	Valid       bool   `json:"valid"`
	BrokenCount int    `json:"broken_count,omitempty"`
	Reason      string `json:"reason,omitempty"`
}
