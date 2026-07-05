package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	fact0 "github.com/fact0-ai/fact0/sdk/go"
)

// PolicyCache is the on-disk copy of the tenant's server-managed governance
// policy. The synchronous PreToolUse path reads ONLY this file — never the
// network — so enforcement adds pure disk I/O to the hot path. Refreshes
// happen on session-start (forced) and stop (when stale).
type PolicyCache struct {
	Version   int          `json:"version"`
	Enforce   bool         `json:"enforce"`
	Rules     []PolicyRule `json:"rules"`
	FetchedAt time.Time    `json:"fetched_at"`
}

func policyCachePath(cfg Config) string {
	return filepath.Join(cfg.StateDir, "policy-cache.json")
}

// LoadPolicyCache reads the cached remote policy from disk. Pure disk read;
// returns (nil, false) when remote policy is disabled, or the file is
// missing/corrupt.
func LoadPolicyCache(cfg Config) (*PolicyCache, bool) {
	if !cfg.RemotePolicy {
		return nil, false
	}
	data, err := os.ReadFile(policyCachePath(cfg))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	var pc PolicyCache
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, false
	}
	return &pc, true
}

// SavePolicyCache atomically persists the cache (temp file + rename, 0600).
func SavePolicyCache(cfg Config, pc *PolicyCache) error {
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(pc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cfg.StateDir, ".policy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, policyCachePath(cfg))
}

// RefreshRemotePolicy fetches the server policy when the cache is older than
// maxAge (0 forces a fetch). Fully fail-open: any error keeps the stale cache
// and is only logged. Never called from the PreToolUse path.
func RefreshRemotePolicy(ctx context.Context, client *fact0.Client, cfg Config, maxAge time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			logf("policy refresh panic recovered: %v", r)
		}
	}()
	if !cfg.RemotePolicy || cfg.APIKey == "" {
		return
	}

	cached, ok := LoadPolicyCache(cfg)
	if ok && maxAge > 0 && time.Since(cached.FetchedAt) < maxAge {
		return
	}
	haveVersion := 0
	if ok {
		haveVersion = cached.Version
	}

	resp, notModified, err := client.Governance.ClaudeCodePolicy(ctx, haveVersion)
	switch {
	case err != nil:
		logf("policy refresh failed (keeping cached rules): %v", err)
	case notModified:
		if ok {
			cached.FetchedAt = time.Now().UTC()
			if err := SavePolicyCache(cfg, cached); err != nil {
				logf("policy cache touch failed: %v", err)
			}
		}
	default:
		rules := make([]PolicyRule, 0, len(resp.Rules))
		for _, r := range resp.Rules {
			rules = append(rules, PolicyRule{Match: r.Match, Tool: r.Tool, Action: r.Action, Reason: r.Reason})
		}
		pc := &PolicyCache{
			Version:   resp.Version,
			Enforce:   resp.Enforce,
			Rules:     rules,
			FetchedAt: time.Now().UTC(),
		}
		if err := SavePolicyCache(cfg, pc); err != nil {
			logf("policy cache write failed: %v", err)
		} else if resp.Version > haveVersion {
			logf("governance policy updated to v%d (%d rules)", resp.Version, len(rules))
		}
	}
}
