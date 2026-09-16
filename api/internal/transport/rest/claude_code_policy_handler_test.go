package rest

import (
	"context"
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

// fakeCCPolicyStore is an in-memory CAS store.
type fakeCCPolicyStore struct {
	policies map[string]*audit.ClaudeCodePolicy
}

func (f *fakeCCPolicyStore) GetClaudeCodePolicy(_ context.Context, tenantID string) (*audit.ClaudeCodePolicy, error) {
	if p, ok := f.policies[tenantID]; ok {
		cp := *p
		return &cp, nil
	}
	return &audit.ClaudeCodePolicy{Version: 0, Rules: []audit.ClaudeCodePolicyRule{}}, nil
}

func (f *fakeCCPolicyStore) PutClaudeCodePolicy(_ context.Context, tenantID string, expectedVersion int,
	enforce bool, rules []audit.ClaudeCodePolicyRule, updatedBy string) (*audit.ClaudeCodePolicy, error) {
	cur, exists := f.policies[tenantID]
	switch {
	case expectedVersion == 0 && exists,
		expectedVersion > 0 && (!exists || cur.Version != expectedVersion):
		return nil, &apperrors.Error{Code: apperrors.CodeConflict, Message: "policy version conflict"}
	}
	next := &audit.ClaudeCodePolicy{
		Version: expectedVersion + 1, Enforce: enforce, Rules: rules,
		UpdatedAt: time.Now().UTC(), UpdatedBy: updatedBy,
	}
	if f.policies == nil {
		f.policies = map[string]*audit.ClaudeCodePolicy{}
	}
	f.policies[tenantID] = next
	cp := *next
	return &cp, nil
}

// fakePolicyTenants resolves any org to a fixed tenant.
type fakePolicyTenants struct{ tenantID string }

func (f *fakePolicyTenants) GetTenantByExternalID(_ context.Context, _, _ string) (*audit.Tenant, error) {
	return &audit.Tenant{ID: f.tenantID}, nil
}

// fakeAuditSink records appended events.
type fakeAuditSink struct{ events []*audit.AuditEvent }

func (f *fakeAuditSink) Log(_ context.Context, _ string, in *audit.AuditEvent) (*audit.AuditEvent, error) {
	f.events = append(f.events, in)
	return in, nil
}

// principalMW injects a fake verified principal (test-only stand-in for RequireJWT).
func principalMW(p *auth.Principal) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
}

func newPolicyHarness(t *testing.T, role string) (*httptest.Server, *fakeCCPolicyStore, *fakeAuditSink, string) {
	t.Helper()
	store := &fakeCCPolicyStore{policies: map[string]*audit.ClaudeCodePolicy{}}
	sink := &fakeAuditSink{}
	h := NewClaudeCodePolicyHandler(store, &fakePolicyTenants{tenantID: "tenant_pol"}, sink, zerolog.Nop())

	// Collector surface behind API-key auth.
	keyStore := &fakeKeyStore{keys: map[string]*audit.APIKey{}}
	rawToken, hashStr, err := auth.GenerateKey("akid_pol")
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	_ = keyStore.CreateKey(context.Background(), &audit.APIKey{
		ID: "akid_pol", TenantID: "tenant_pol", Hash: hashStr, Scope: audit.ScopeRead,
	})

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.APIKey(keyStore))
		h.MountRead(r)
	})
	r.Group(func(r chi.Router) {
		r.Use(principalMW(&auth.Principal{UserID: "user_1", OrgID: "org_1", Role: role}))
		h.MountDashboard(r)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, store, sink, rawToken
}

func doReq(t *testing.T, method, url, token, body string) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestCCPolicy_CollectorFetch(t *testing.T) {
	t.Parallel()
	srv, store, _, token := newPolicyHarness(t, "admin")

	// Empty tenant -> version-0 sentinel.
	code, body, _ := doReq(t, "GET", srv.URL+"/api/v1/integrations/claude-code/policy", token, "")
	if code != 200 || !strings.Contains(body, `"version":0`) {
		t.Fatalf("empty fetch: %d %s", code, body)
	}

	store.policies["tenant_pol"] = &audit.ClaudeCodePolicy{
		Version: 3, Enforce: true,
		Rules: []audit.ClaudeCodePolicyRule{{Match: "x", Action: "deny"}},
	}

	code, body, hdr := doReq(t, "GET", srv.URL+"/api/v1/integrations/claude-code/policy", token, "")
	if code != 200 || !strings.Contains(body, `"version":3`) || hdr.Get("ETag") != `"v3"` {
		t.Fatalf("fetch: %d %s etag=%q", code, body, hdr.Get("ETag"))
	}

	// Cheap polling: matching version -> 304.
	code, _, _ = doReq(t, "GET", srv.URL+"/api/v1/integrations/claude-code/policy?version=3", token, "")
	if code != http.StatusNotModified {
		t.Errorf("?version=3 should 304, got %d", code)
	}
	code, _, _ = doReq(t, "GET", srv.URL+"/api/v1/integrations/claude-code/policy?version=2", token, "")
	if code != 200 {
		t.Errorf("?version=2 should 200, got %d", code)
	}

	// No credentials -> 401.
	code, _, _ = doReq(t, "GET", srv.URL+"/api/v1/integrations/claude-code/policy", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("unauthenticated fetch should 401, got %d", code)
	}
}

func TestCCPolicy_DashboardPut(t *testing.T) {
	t.Parallel()
	srv, _, sink, _ := newPolicyHarness(t, "admin")

	// Create v1 + audit event.
	code, body, _ := doReq(t, "PUT", srv.URL+"/v1/me/governance/policy", "",
		`{"version":0,"enforce":true,"rules":[{"match":"rm\\s+-rf","tool":"Bash","action":"deny","reason":"no"}]}`)
	if code != 200 || !strings.Contains(body, `"version":1`) {
		t.Fatalf("create: %d %s", code, body)
	}
	if len(sink.events) != 1 || sink.events[0].Action != "claude_code.policy.updated" ||
		sink.events[0].Actor.ID != "user_1" {
		t.Errorf("expected one policy.updated audit event by user_1, got %+v", sink.events)
	}

	// Invalid regexp -> 400, no extra audit event.
	code, body, _ = doReq(t, "PUT", srv.URL+"/v1/me/governance/policy", "",
		`{"version":1,"enforce":false,"rules":[{"match":"([","action":"deny"}]}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "invalid regexp") {
		t.Errorf("bad regexp: %d %s", code, body)
	}
	if len(sink.events) != 1 {
		t.Errorf("invalid rule must not append audit events, got %d", len(sink.events))
	}

	// Wrong action -> 400.
	code, _, _ = doReq(t, "PUT", srv.URL+"/v1/me/governance/policy", "",
		`{"version":1,"enforce":false,"rules":[{"match":"x","action":"allow"}]}`)
	if code != http.StatusBadRequest {
		t.Errorf("non-deny action should 400, got %d", code)
	}

	// Stale version -> 409.
	code, _, _ = doReq(t, "PUT", srv.URL+"/v1/me/governance/policy", "",
		`{"version":0,"enforce":false,"rules":[]}`)
	if code != http.StatusConflict {
		t.Errorf("stale version should 409, got %d", code)
	}

	// GET returns the saved policy with updated_by.
	code, body, _ = doReq(t, "GET", srv.URL+"/v1/me/governance/policy", "", "")
	if code != 200 || !strings.Contains(body, `"updated_by":"user_1"`) {
		t.Errorf("dashboard get: %d %s", code, body)
	}
}

func TestCCPolicy_MemberCannotWrite(t *testing.T) {
	t.Parallel()
	srv, _, _, _ := newPolicyHarness(t, "member")
	code, _, _ := doReq(t, "PUT", srv.URL+"/v1/me/governance/policy", "",
		`{"version":0,"enforce":false,"rules":[]}`)
	if code != http.StatusForbidden {
		t.Errorf("member PUT should 403, got %d", code)
	}
	// Read stays allowed.
	code, _, _ = doReq(t, "GET", srv.URL+"/v1/me/governance/policy", "", "")
	if code != 200 {
		t.Errorf("member GET should 200, got %d", code)
	}
}
