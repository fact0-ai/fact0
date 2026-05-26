package fact0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const userAgent = "fact0-go/1.0.0"

// DefaultBaseURL is the production Fact0 API origin.
// Override via Config.BaseURL for local development or private deployments.
const DefaultBaseURL = "https://api.fact0.io"

// Config configures the Fact0 client.
type Config struct {
	APIKey     string
	BaseURL    string
	SyncIngest bool
	Timeout    time.Duration
	MaxRetries int
}

// Client is the unified Fact0 SDK client.
type Client struct {
	http   *http.Client
	cfg    Config
	Audit  *AuditClient
	Telemetry *TelemetryClient
}

// NewClient constructs a Fact0 client.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("FACT0_API_KEY")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	c := &Client{
		http: &http.Client{Timeout: cfg.Timeout},
		cfg:  cfg,
	}
	c.Audit = &AuditClient{parent: c}
	c.Telemetry = &TelemetryClient{parent: c}
	return c
}

func (c *Client) doJSON(ctx context.Context, method, path string, in any, out any, auth bool) error {
	var payload []byte
	if in != nil {
		var err error
		payload, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	url := c.cfg.BaseURL + path
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)
		if auth && c.cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		if c.cfg.SyncIngest {
			req.Header.Set("X-Fact0-Sync", "true")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 300 {
			if out == nil {
				return nil
			}
			return json.Unmarshal(data, out)
		}
		if resp.StatusCode != 429 && resp.StatusCode < 500 {
			return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, string(data))
		}
		lastErr = fmt.Errorf("%s %s: %d", method, path, resp.StatusCode)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				time.Sleep(time.Duration(secs) * time.Second)
				continue
			}
		}
		time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
	}
	return lastErr
}
