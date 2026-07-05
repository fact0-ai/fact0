package fact0

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// GovernanceClient wraps governance/policy REST endpoints.
type GovernanceClient struct {
	parent *Client
}

// ClaudeCodePolicyRule is one server-managed governance rule.
type ClaudeCodePolicyRule struct {
	Match  string `json:"match"`
	Tool   string `json:"tool,omitempty"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// ClaudeCodePolicyResponse is the collector-facing policy document.
// Version 0 means "no policy configured for this tenant".
type ClaudeCodePolicyResponse struct {
	Version   int                    `json:"version"`
	Enforce   bool                   `json:"enforce"`
	Rules     []ClaudeCodePolicyRule `json:"rules"`
	UpdatedAt string                 `json:"updated_at"`
}

// ClaudeCodePolicy fetches the tenant's governance policy. When haveVersion
// is > 0 it is sent as ?version= so an unchanged policy returns
// (nil, true, nil) — a 304-style cheap poll. Single attempt, no retries: the
// caller (the fail-open collector) refreshes opportunistically.
func (g *GovernanceClient) ClaudeCodePolicy(ctx context.Context, haveVersion int) (*ClaudeCodePolicyResponse, bool, error) {
	url := g.parent.cfg.BaseURL + "/api/v1/integrations/claude-code/policy"
	if haveVersion > 0 {
		url += "?version=" + strconv.Itoa(haveVersion)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	if g.parent.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.parent.cfg.APIKey)
	}

	resp, err := g.parent.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, true, nil
	case resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, false, fmt.Errorf("GET claude-code policy: %d %s", resp.StatusCode, string(body))
	}

	var out ClaudeCodePolicyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false, err
	}
	return &out, false, nil
}
