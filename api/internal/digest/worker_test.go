package digest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
)

type fakeSummaryStore struct {
	summaries map[string]*audit.ClaudeCodeWeeklySummary
}

func (f *fakeSummaryStore) GetClaudeCodeWeeklySummary(_ context.Context, tenantID string, _, _ time.Time) (*audit.ClaudeCodeWeeklySummary, error) {
	if s, ok := f.summaries[tenantID]; ok {
		return s, nil
	}
	return &audit.ClaudeCodeWeeklySummary{TopRepos: []audit.ClaudeCodeRepoCount{}}, nil
}

type sentEmail struct {
	template, to string
	props        map[string]any
}

type fakeEmail struct{ sent []sentEmail }

func (f *fakeEmail) Send(_ context.Context, template, to string, props map[string]any) error {
	f.sent = append(f.sent, sentEmail{template, to, props})
	return nil
}

func testWorker(candidates []candidate, store *fakeSummaryStore, email *fakeEmail) (*Worker, *[]WebhookPayload, *[]string) {
	var hooks []WebhookPayload
	var claimed []string
	w := &Worker{
		store:  store,
		email:  email,
		cfg:    Config{Interval: time.Hour, Period: 7 * 24 * time.Hour, MinInterval: 156 * time.Hour},
		logger: zerolog.Nop(),
		now:    func() time.Time { return time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC) },
		webhookPost: func(_ context.Context, _ string, payload any) error {
			hooks = append(hooks, payload.(WebhookPayload))
			return nil
		},
	}
	w.candidatesFn = func(context.Context) ([]candidate, error) { return candidates, nil }
	w.claimFn = func(_ context.Context, tenantID string, _ time.Time) (bool, error) {
		claimed = append(claimed, tenantID)
		return true, nil
	}
	return w, &hooks, &claimed
}

func TestRunOnce_DeliversBothChannels(t *testing.T) {
	store := &fakeSummaryStore{summaries: map[string]*audit.ClaudeCodeWeeklySummary{
		"t1": {Sessions: 5, Prompts: 40, ToolCalls: 200, FilesTouched: 12, Commands: 80,
			Denied: 2, CostUSD: 3.5, Tokens: 900_000,
			TopRepos: []audit.ClaudeCodeRepoCount{{Cwd: "/repo/a", Sessions: 3}}},
	}}
	email := &fakeEmail{}
	w, hooks, claimed := testWorker([]candidate{
		{TenantID: "t1", TenantName: "Acme", AlertEmail: "dev@acme.io", WebhookURL: "https://hooks.acme.io/x"},
	}, store, email)

	w.runOnce(context.Background())

	if len(*claimed) != 1 || (*claimed)[0] != "t1" {
		t.Fatalf("claim not taken: %v", *claimed)
	}
	if len(email.sent) != 1 || email.sent[0].template != "cc-weekly-digest" || email.sent[0].to != "dev@acme.io" {
		t.Fatalf("email wrong: %+v", email.sent)
	}
	if email.sent[0].props["sessions"] != 5 || email.sent[0].props["workspaceName"] != "Acme" {
		t.Errorf("email props wrong: %+v", email.sent[0].props)
	}
	if len(*hooks) != 1 || (*hooks)[0].Type != "coding_agents_digest" || (*hooks)[0].Stats.Sessions != 5 {
		t.Fatalf("webhook wrong: %+v", *hooks)
	}
}

func TestRunOnce_ChannelSelectionAndZeroSessions(t *testing.T) {
	store := &fakeSummaryStore{summaries: map[string]*audit.ClaudeCodeWeeklySummary{
		"email-only":   {Sessions: 2},
		"webhook-only": {Sessions: 3},
		"quiet":        {Sessions: 0},
	}}
	email := &fakeEmail{}
	w, hooks, _ := testWorker([]candidate{
		{TenantID: "email-only", AlertEmail: "a@b.c"},
		{TenantID: "webhook-only", WebhookURL: "https://h/x"},
		{TenantID: "quiet", AlertEmail: "q@b.c", WebhookURL: "https://h/q"},
	}, store, email)

	w.runOnce(context.Background())

	if len(email.sent) != 1 || email.sent[0].to != "a@b.c" {
		t.Errorf("expected exactly the email-only tenant emailed: %+v", email.sent)
	}
	if len(*hooks) != 1 || (*hooks)[0].TenantID != "webhook-only" {
		t.Errorf("expected exactly the webhook-only tenant hooked: %+v", *hooks)
	}
}

func TestRunOnce_LostClaimSkipsAndErrorsDoNotAbort(t *testing.T) {
	store := &fakeSummaryStore{summaries: map[string]*audit.ClaudeCodeWeeklySummary{
		"t1": {Sessions: 1}, "t2": {Sessions: 1},
	}}
	email := &fakeEmail{}
	w, _, _ := testWorker([]candidate{
		{TenantID: "t1", AlertEmail: "a@b.c"},
		{TenantID: "t2", AlertEmail: "b@b.c"},
	}, store, email)
	// t1's claim is lost to another replica; t2's summary errors.
	w.claimFn = func(_ context.Context, tenantID string, _ time.Time) (bool, error) {
		return tenantID != "t1", nil
	}
	failing := &failingSummaryStore{}
	w.store = failing

	w.runOnce(context.Background()) // must not panic or abort the loop

	if len(email.sent) != 0 {
		t.Errorf("no digests should have been sent: %+v", email.sent)
	}
	if failing.calls != 1 {
		t.Errorf("summary should be computed only for the claimed tenant, got %d calls", failing.calls)
	}
}

type failingSummaryStore struct{ calls int }

func (f *failingSummaryStore) GetClaudeCodeWeeklySummary(context.Context, string, time.Time, time.Time) (*audit.ClaudeCodeWeeklySummary, error) {
	f.calls++
	return nil, errors.New("boom")
}
