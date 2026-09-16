package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// fakeCCSessionStore records the filter it received and returns canned rows.
type fakeCCSessionStore struct {
	sessions   []*audit.ClaudeCodeSession
	lastTenant string
	lastFilter audit.ClaudeCodeSessionFilter
}

func (f *fakeCCSessionStore) ListClaudeCodeSessions(_ context.Context, tenantID string, filter audit.ClaudeCodeSessionFilter) ([]*audit.ClaudeCodeSession, int, error) {
	f.lastTenant = tenantID
	f.lastFilter = filter
	return f.sessions, len(f.sessions), nil
}

func (f *fakeCCSessionStore) GetClaudeCodeSession(_ context.Context, tenantID, sessionID string) (*audit.ClaudeCodeSession, error) {
	f.lastTenant = tenantID
	for _, s := range f.sessions {
		if s.SessionID == sessionID {
			return s, nil
		}
	}
	return nil, apperrors.NotFound("claude_code_session", sessionID)
}

// newCCSessionsHarness serves the handler behind the same auth.APIKey chain
// the integrations harness uses; TenantFromContext resolution is identical to
// the DualAuth group in main.go.
func newCCSessionsHarness(t *testing.T, store *fakeCCSessionStore) (*httptest.Server, string) {
	t.Helper()
	keyStore := &fakeKeyStore{keys: map[string]*audit.APIKey{}}
	rawToken, hashStr, err := auth.GenerateKey("akid_ccs")
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	_ = keyStore.CreateKey(context.Background(), &audit.APIKey{
		ID:       "akid_ccs",
		TenantID: "tenant_ccs",
		Hash:     hashStr,
		Scope:    audit.ScopeRead,
	})

	h := NewClaudeCodeSessionsHandler(store, zerolog.Nop())
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.APIKey(keyStore))
		h.Mount(r)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, rawToken
}

func ccGET(t *testing.T, srv *httptest.Server, token, path string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestCCSessions_ListShape(t *testing.T) {
	t.Parallel()
	ended := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	store := &fakeCCSessionStore{sessions: []*audit.ClaudeCodeSession{{
		SessionID: "sess-1", Cwd: "/repo", Status: "completed",
		StartedAt: ended.Add(-time.Hour), EndedAt: &ended, LastActivityAt: ended,
		Prompts: 3, ToolCalls: 12, FilesTouched: 4, Commands: 5, Denied: 1,
		CostUSD: 0.42, Tokens: 1234, ExecutionID: "exec_1",
	}}}
	srv, token := newCCSessionsHarness(t, store)

	code, body := ccGET(t, srv, token, "/api/v1/integrations/claude-code/sessions")
	if code != http.StatusOK {
		t.Fatalf("status %d, body: %s", code, body)
	}
	var out struct {
		Sessions []map[string]any `json:"sessions"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if out.Total != 1 || len(out.Sessions) != 1 || out.Page != 1 || out.PageSize != 50 {
		t.Errorf("unexpected envelope: %+v", out)
	}
	if out.Sessions[0]["session_id"] != "sess-1" || out.Sessions[0]["denied"] != float64(1) {
		t.Errorf("unexpected session row: %+v", out.Sessions[0])
	}
	if store.lastTenant != "tenant_ccs" {
		t.Errorf("tenant not propagated, got %q", store.lastTenant)
	}
}

func TestCCSessions_EmptyListIsArray(t *testing.T) {
	t.Parallel()
	srv, token := newCCSessionsHarness(t, &fakeCCSessionStore{})
	code, body := ccGET(t, srv, token, "/api/v1/integrations/claude-code/sessions")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(body, `"sessions":[]`) {
		t.Errorf("empty list must serialize as [], got: %s", body)
	}
}

func TestCCSessions_BadFromIs400(t *testing.T) {
	t.Parallel()
	srv, token := newCCSessionsHarness(t, &fakeCCSessionStore{})
	code, body := ccGET(t, srv, token, "/api/v1/integrations/claude-code/sessions?from=yesterday")
	if code != http.StatusBadRequest || !strings.Contains(body, "from must be RFC3339") {
		t.Errorf("expected 400 RFC3339 error, got %d: %s", code, body)
	}
}

func TestCCSessions_FilterPassthrough(t *testing.T) {
	t.Parallel()
	store := &fakeCCSessionStore{}
	srv, token := newCCSessionsHarness(t, store)
	code, _ := ccGET(t, srv, token,
		"/api/v1/integrations/claude-code/sessions?cwd=/repo&page=2&page_size=999&from=2026-07-01T00:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	f := store.lastFilter
	if f.Cwd != "/repo" || f.Page != 2 || f.PageSize != 200 /* clamped */ ||
		!f.From.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("filter not propagated/clamped: %+v", f)
	}
}

func TestCCSessions_Unauthorized(t *testing.T) {
	t.Parallel()
	srv, _ := newCCSessionsHarness(t, &fakeCCSessionStore{})
	code, _ := ccGET(t, srv, "", "/api/v1/integrations/claude-code/sessions")
	if code != http.StatusUnauthorized {
		t.Errorf("expected 401 without credentials, got %d", code)
	}
}

func TestCCSessions_DetailFoundAndNotFound(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	store := &fakeCCSessionStore{sessions: []*audit.ClaudeCodeSession{{
		SessionID: "sess-9", Status: "active", StartedAt: now, LastActivityAt: now,
	}}}
	srv, token := newCCSessionsHarness(t, store)

	code, body := ccGET(t, srv, token, "/api/v1/integrations/claude-code/sessions/sess-9")
	if code != http.StatusOK || !strings.Contains(body, `"session_id":"sess-9"`) {
		t.Errorf("detail: expected 200 with session, got %d: %s", code, body)
	}
	code, _ = ccGET(t, srv, token, "/api/v1/integrations/claude-code/sessions/nope")
	if code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown session, got %d", code)
	}
}
