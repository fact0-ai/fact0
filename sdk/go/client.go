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

const userAgent = "fact0-go/1.0.4"

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

// Client is the unified Fact0 SDK client. Numbers in generic JSON response
// maps use json.Number, preserving captured integers and decimals exactly.
// Use Number.Int64 or Number.Float64 when arithmetic is required.
type Client struct {
	http       *http.Client
	cfg        Config
	Audit      *AuditClient
	Telemetry  *TelemetryClient
	Governance *GovernanceClient
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
	c.Governance = &GovernanceClient{parent: c}
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
			if attempt < c.cfg.MaxRetries {
				if err := retryWait(ctx, time.Duration(200*(1<<attempt))*time.Millisecond); err != nil {
					return err
				}
			}
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < c.cfg.MaxRetries {
				if err := retryWait(ctx, time.Duration(200*(1<<attempt))*time.Millisecond); err != nil {
					return err
				}
			}
			continue
		}
		if resp.StatusCode < 300 {
			if out == nil {
				return nil
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(out); err != nil {
				return err
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return fmt.Errorf("invalid JSON response: expected one value")
			}
			return nil
		}
		if resp.StatusCode != 429 && resp.StatusCode < 500 {
			return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, string(data))
		}
		lastErr = fmt.Errorf("%s %s: %d", method, path, resp.StatusCode)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 && secs <= int((1<<63-1)/int64(time.Second)) {
				if attempt < c.cfg.MaxRetries {
					if err := retryWait(ctx, time.Duration(secs)*time.Second); err != nil {
						return err
					}
				}
				continue
			}
		}
		if attempt < c.cfg.MaxRetries {
			if err := retryWait(ctx, time.Duration(200*(1<<attempt))*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return lastErr
}

// retryWait honors the caller's deadline even when Retry-After is much longer.
func retryWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
