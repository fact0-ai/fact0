package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/execution"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

var ccEventSeq int

func ccEvent(_, action, sessionID string, at time.Time, md map[string]interface{}, res *audit.Resource) *audit.AuditEvent {
	ccEventSeq++
	e := &audit.AuditEvent{
		ID:        fmt.Sprintf("evt_ccs_%d_%d", time.Now().UnixNano(), ccEventSeq),
		Actor:     audit.Actor{ID: "dev-1", Type: audit.ActorHuman},
		Action:    action,
		Resource:  audit.Resource{ID: sessionID, Type: "claude_code.session"},
		Outcome:   audit.OutcomeSuccess,
		Timestamp: at,
		Metadata:  md,
	}
	if res != nil {
		e.Resource = *res
	}
	return e
}

// TestListClaudeCodeSessions_Aggregation seeds two sessions (one completed
// with a linked execution + spans, one active) and asserts the rollup.
func TestListClaudeCodeSessions_Aggregation(t *testing.T) {
	tstore, store, closeFn := testStore(t)
	defer closeFn()
	ctx := context.Background()

	tenant := fmt.Sprintf("tnt_test_ccs_%d", time.Now().UnixNano())
	other := tenant + "_other"
	ensureTenant(t, store, tenant)
	ensureTenant(t, store, other)

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	sid, sid2 := "cc-sess-1", "cc-sess-2"
	execID := "exec_ccs_" + fmt.Sprint(time.Now().UnixNano())

	toolRes := func(rid, rtype string) *audit.Resource { return &audit.Resource{ID: rid, Type: rtype} }
	events := []*audit.AuditEvent{
		// Session 1: completed, 2 prompts, 4 tool calls (2 distinct files + 1 dup, 1 command)
		ccEvent("", "claude_code.session.start", sid, base, map[string]interface{}{
			"cwd": "/repo/alpha", "permission_mode": "default", "source": "startup", "execution_id": execID}, nil),
		ccEvent("", "claude_code.prompt.submit", sid, base.Add(1*time.Minute), map[string]interface{}{"prompt_len": 10}, nil),
		ccEvent("", "claude_code.prompt.submit", sid, base.Add(2*time.Minute), map[string]interface{}{"prompt_len": 20}, nil),
		ccEvent("", "claude_code.tool.edit", sid, base.Add(3*time.Minute), map[string]interface{}{"session_id": sid}, toolRes("/repo/alpha/a.go", "file")),
		ccEvent("", "claude_code.tool.edit", sid, base.Add(4*time.Minute), map[string]interface{}{"session_id": sid}, toolRes("/repo/alpha/b.go", "file")),
		ccEvent("", "claude_code.tool.write", sid, base.Add(5*time.Minute), map[string]interface{}{"session_id": sid}, toolRes("/repo/alpha/a.go", "file")),
		ccEvent("", "claude_code.tool.bash", sid, base.Add(6*time.Minute), map[string]interface{}{"session_id": sid}, toolRes("cmdhash", "shell.command")),
		ccEvent("", "claude_code.session.end", sid, base.Add(30*time.Minute), map[string]interface{}{"reason": "exit", "execution_id": execID}, nil),
		// Session 2: recent activity, no end -> active; carries a git branch.
		ccEvent("", "claude_code.session.start", sid2, base.Add(40*time.Minute), map[string]interface{}{
			"cwd": "/repo/beta", "permission_mode": "plan", "source": "startup", "git_branch": "feat/x"}, nil),
		ccEvent("", "claude_code.prompt.submit", sid2, base.Add(41*time.Minute), map[string]interface{}{"prompt_len": 5}, nil),
	}
	// Session 1 also has one FAILED tool call.
	failedTool := ccEvent("", "claude_code.tool.bash", sid, base.Add(6*time.Minute+30*time.Second),
		map[string]interface{}{"session_id": sid}, toolRes("failhash", "shell.command"))
	failedTool.Outcome = audit.OutcomeError
	events = append(events, failedTool)
	// Session 3: last activity an hour ago, never ended -> idle.
	sid3 := "cc-sess-3"
	events = append(events, ccEvent("", "claude_code.session.start", sid3, base, map[string]interface{}{
		"cwd": "/repo/gamma"}, nil))
	if _, err := store.AppendBatchChained(ctx, tenant, events); err != nil {
		t.Fatalf("seed events: %v", err)
	}

	// Telemetry for session 1: 1 deny + 1 allow policy span, 1 model invocation.
	if err := tstore.InsertExecution(ctx, tenant, &execution.Execution{
		ID: execID, AgentID: "claude-code", Status: execution.StatusRunning, StartedAt: base,
		Metadata: map[string]string{"cwd": "/repo/alpha"},
	}); err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	endAt := base.Add(7 * time.Minute)
	// Span ids must be unique per run: InsertSpan is ON CONFLICT DO NOTHING,
	// so reused ids silently stay attached to a previous run's execution.
	sid1 := fmt.Sprintf("span_ccs_%d", time.Now().UnixNano())
	spans := []*execution.Span{
		{ID: sid1 + "_deny", ExecutionID: execID, SpanType: "POLICY_EVALUATION", Name: "policy:Bash",
			Status: execution.StatusCompleted, StartedAt: base, EndedAt: &endAt,
			Metadata: map[string]string{"decision": "deny", "tool": "Bash"}},
		{ID: sid1 + "_allow", ExecutionID: execID, SpanType: "POLICY_EVALUATION", Name: "policy:Edit",
			Status: execution.StatusCompleted, StartedAt: base, EndedAt: &endAt,
			Metadata: map[string]string{"decision": "allow", "tool": "Edit"}},
		{ID: sid1 + "_mi", ExecutionID: execID, SpanType: "MODEL_INVOCATION", Name: "model_invocation",
			Status: execution.StatusCompleted, StartedAt: base, EndedAt: &endAt,
			ModelInvocation: &execution.ModelInvocationDetail{ModelName: "claude", CostUSD: 0.5, TotalTokens: 1000}},
	}
	for _, sp := range spans {
		if err := tstore.InsertSpan(ctx, sp); err != nil {
			t.Fatalf("seed span %s: %v", sp.ID, err)
		}
	}

	sessions, total, err := store.ListClaudeCodeSessions(ctx, tenant, audit.ClaudeCodeSessionFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got total=%d len=%d", total, len(sessions))
	}
	// Ordering: latest activity first -> session 2, then 1, then 3.
	if sessions[0].SessionID != sid2 || sessions[1].SessionID != sid || sessions[2].SessionID != sid3 {
		t.Fatalf("wrong order: [%s %s %s]", sessions[0].SessionID, sessions[1].SessionID, sessions[2].SessionID)
	}

	s1 := sessions[1]
	if s1.Prompts != 2 || s1.ToolCalls != 5 || s1.FilesTouched != 2 || s1.Commands != 2 {
		t.Errorf("session 1 counts wrong: %+v", s1)
	}
	if s1.Failures != 1 {
		t.Errorf("session 1 failures = %d, want 1", s1.Failures)
	}
	if s1.Status != "completed" || s1.EndedAt == nil || s1.Cwd != "/repo/alpha" || s1.ExecutionID != execID {
		t.Errorf("session 1 identity wrong: %+v", s1)
	}
	if s1.Denied != 1 || s1.CostUSD != 0.5 || s1.Tokens != 1000 {
		t.Errorf("session 1 span rollups wrong: denied=%d cost=%v tokens=%d", s1.Denied, s1.CostUSD, s1.Tokens)
	}
	s2 := sessions[0]
	if s2.Status != "active" || s2.EndedAt != nil || s2.Cwd != "/repo/beta" || s2.Denied != 0 {
		t.Errorf("session 2 wrong: %+v", s2)
	}
	if s2.GitBranch != "feat/x" {
		t.Errorf("session 2 git branch = %q, want feat/x", s2.GitBranch)
	}
	// Session 3 never ended and has been silent for ~1h -> idle, not active.
	if sessions[2].Status != "idle" {
		t.Errorf("session 3 status = %q, want idle", sessions[2].Status)
	}

	// cwd filter
	filtered, ftotal, err := store.ListClaudeCodeSessions(ctx, tenant, audit.ClaudeCodeSessionFilter{Cwd: "/repo/alpha"})
	if err != nil || ftotal != 1 || len(filtered) != 1 || filtered[0].SessionID != sid {
		t.Errorf("cwd filter: err=%v total=%d sessions=%+v", err, ftotal, filtered)
	}

	// Tenant isolation: other tenant sees nothing, and a forged execution_id
	// referencing tenant 1's execution contributes no span stats.
	forged := ccEvent("", "claude_code.session.start", "cc-forged", base, map[string]interface{}{
		"cwd": "/evil", "execution_id": execID}, nil)
	if _, err := store.AppendBatchChained(ctx, other, []*audit.AuditEvent{forged}); err != nil {
		t.Fatalf("seed forged: %v", err)
	}
	osessions, ototal, err := store.ListClaudeCodeSessions(ctx, other, audit.ClaudeCodeSessionFilter{})
	if err != nil || ototal != 1 {
		t.Fatalf("other tenant list: err=%v total=%d", err, ototal)
	}
	if osessions[0].Denied != 0 || osessions[0].CostUSD != 0 || osessions[0].Tokens != 0 {
		t.Errorf("forged execution_id leaked span stats across tenants: %+v", osessions[0])
	}

	// Get: found + not found
	got, err := store.GetClaudeCodeSession(ctx, tenant, sid)
	if err != nil || got.SessionID != sid || got.Prompts != 2 {
		t.Errorf("get: err=%v session=%+v", err, got)
	}
	var appErr *apperrors.Error
	if _, err := store.GetClaudeCodeSession(ctx, tenant, "does-not-exist"); !errors.As(err, &appErr) || appErr.Code != apperrors.CodeNotFound {
		t.Errorf("expected NotFound, got %v", err)
	}

	// Weekly summary over the same seeded data: 3 sessions, tenant-wide
	// distinct files, span rollups, top repos ordered by session count.
	sum, err := store.GetClaudeCodeWeeklySummary(ctx, tenant, base.Add(-time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatalf("weekly summary: %v", err)
	}
	if sum.Sessions != 3 || sum.Prompts != 3 || sum.FilesTouched != 2 || sum.Commands != 2 {
		t.Errorf("summary totals wrong: %+v", sum)
	}
	if sum.Denied != 1 || sum.CostUSD != 0.5 || sum.Tokens != 1000 {
		t.Errorf("summary span rollups wrong: %+v", sum)
	}
	if len(sum.TopRepos) != 3 || sum.TopRepos[0].Sessions != 1 {
		t.Errorf("top repos wrong: %+v", sum.TopRepos)
	}
	// Other tenant's summary is isolated (its one forged session, no spans).
	osum, err := store.GetClaudeCodeWeeklySummary(ctx, other, base.Add(-time.Hour), time.Now().UTC())
	if err != nil || osum.Sessions != 1 || osum.Denied != 0 || osum.CostUSD != 0 {
		t.Errorf("other tenant summary isolation: err=%v %+v", err, osum)
	}
}
