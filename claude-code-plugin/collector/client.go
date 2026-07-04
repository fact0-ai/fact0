package main

import (
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// NewFact0 constructs a Fact0 SDK client from the collector config.
//
// SyncIngest is enabled so that events are flushed before the short-lived
// collector process exits (hooks are fire-and-forget subprocesses). The
// per-request Timeout is kept tight; the overall deadline is governed by the
// context created in main.
func NewFact0(cfg Config) *fact0.Client {
	return fact0.NewClient(fact0.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		SyncIngest: true,
		Timeout:    8 * time.Second,
		MaxRetries: 2,
	})
}
