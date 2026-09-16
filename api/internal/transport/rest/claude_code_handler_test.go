package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
)

// newIntegrationsHarness mirrors newHarness but wires MountIntegrations under
// the same auth.APIKey + write-scope chain main.go uses, so the agentless
// Claude Code endpoint is reachable in tests.
func newIntegrationsHarness(t *testing.T, tenants ...string) *harness {
	t.Helper()
	repo := newFakeRepo()
	keyStore := &fakeKeyStore{keys: map[string]*audit.APIKey{}}
	rawKeys := map[string]string{}

	for _, tid := range tenants {
		rawToken, hashStr, err := auth.GenerateKey("akid_" + tid)
		if err != nil {
			t.Fatalf("genkey: %v", err)
		}
		_ = keyStore.CreateKey(context.Background(), &audit.APIKey{
			ID:       "akid_" + tid,
			TenantID: tid,
			Hash:     hashStr,
			Scope:    audit.ScopeWrite,
		})
		rawKeys[tid] = rawToken
	}

	svc := audit.NewService(repo, zerolog.Nop(), false)
	h := NewAuditHandler(svc, nil, nil, zerolog.Nop(), AuditHandlerConfig{})

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.APIKey(keyStore))
		h.MountIntegrations(r)
	})

	return &harness{
		srv:  httptest.NewServer(r),
		repo: repo,
		keys: rawKeys,
	}
}

// TestAPI_IngestClaudeCode_PostToolUse posts a native Claude Code PostToolUse
// hook payload with a valid write-scope key and asserts a 2xx plus a persisted
// audit event whose action carries the "claude_code." prefix.
func TestAPI_IngestClaudeCode_PostToolUse(t *testing.T) {
	t.Parallel()
	h := newIntegrationsHarness(t, "tenant_cc")
	defer h.close()
	key := h.keys["tenant_cc"]

	payload := map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "sess-123",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "ls -la /tmp"},
		"tool_response":   map[string]any{"stdout": "ok"},
		"cwd":             "/home/user/proj",
	}

	resp := h.do(t, "POST", "/v1/integrations/claude-code", key, payload)
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("status = %d, want 2xx", resp.StatusCode)
	}

	rows := h.repo.rows["tenant_cc"]
	if len(rows) != 1 {
		t.Fatalf("persisted rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if !strings.HasPrefix(got.Action, "claude_code.") {
		t.Fatalf("action = %q, want prefix claude_code.", got.Action)
	}
	if got.Action != "claude_code.tool.bash" {
		t.Fatalf("action = %q, want claude_code.tool.bash", got.Action)
	}
	if got.TenantID != "tenant_cc" {
		t.Fatalf("tenant = %q, want tenant_cc", got.TenantID)
	}
	// Bash command must be hashed, never stored raw, when capture_raw is off.
	if got.Metadata["command"] != nil {
		t.Fatalf("raw command leaked into metadata: %v", got.Metadata["command"])
	}
	if _, ok := got.Metadata["command_sha256"]; !ok {
		t.Fatalf("missing command_sha256 in metadata: %+v", got.Metadata)
	}
}
