package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// EmailBridge posts templated transactional emails to the Next.js internal route.
type EmailBridge struct {
	url    string
	secret []byte
	client *http.Client
}

// NewEmailBridge returns nil when url or secret is empty.
func NewEmailBridge(url, secret string) *EmailBridge {
	if url == "" || secret == "" {
		return nil
	}
	return &EmailBridge{
		url:    url,
		secret: []byte(secret),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type emailRequest struct {
	Template string         `json:"template"`
	To       string         `json:"to"`
	Props    map[string]any `json:"props"`
}

func (b *EmailBridge) Send(ctx context.Context, template, to string, props map[string]any) error {
	if b == nil {
		return nil
	}
	body, err := json.Marshal(emailRequest{
		Template: template,
		To:       to,
		Props:    props,
	})
	if err != nil {
		return err
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, b.secret)
	_, _ = mac.Write([]byte(ts))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fact0-Timestamp", ts)
	req.Header.Set("X-Fact0-Signature", sig)

	res, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("email bridge %s: %s", res.Status, string(slurp))
	}
	return nil
}
