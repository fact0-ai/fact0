package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

func isConflict(err error) bool {
	var appErr *apperrors.Error
	return errors.As(err, &appErr) && appErr.Code == apperrors.CodeConflict
}

func TestClaudeCodePolicyCAS(t *testing.T) {
	store, closeFn := testAuditStore(t)
	defer closeFn()
	ctx := context.Background()

	tenant := fmt.Sprintf("tnt_test_ccpol_%d", time.Now().UnixNano())
	ensureTenant(t, store, tenant)

	// Missing row -> version-0 sentinel, not an error.
	p, err := store.GetClaudeCodePolicy(ctx, tenant)
	if err != nil || p.Version != 0 || len(p.Rules) != 0 {
		t.Fatalf("empty get: err=%v policy=%+v", err, p)
	}

	rules := []audit.ClaudeCodePolicyRule{{Match: `rm\s+-rf`, Tool: "Bash", Action: "deny", Reason: "destructive"}}

	// Create with expected version 0 -> version 1.
	v1, err := store.PutClaudeCodePolicy(ctx, tenant, 0, true, rules, "user_a")
	if err != nil || v1.Version != 1 || !v1.Enforce {
		t.Fatalf("create: err=%v policy=%+v", err, v1)
	}

	// Create again over an existing row -> conflict.
	if _, err := store.PutClaudeCodePolicy(ctx, tenant, 0, false, rules, "user_b"); !isConflict(err) {
		t.Errorf("expected conflict on duplicate create, got %v", err)
	}

	// Update with matching version -> version 2.
	v2, err := store.PutClaudeCodePolicy(ctx, tenant, 1, false, nil, "user_b")
	if err != nil || v2.Version != 2 || v2.Enforce {
		t.Fatalf("update: err=%v policy=%+v", err, v2)
	}

	// Stale version -> conflict.
	if _, err := store.PutClaudeCodePolicy(ctx, tenant, 1, true, rules, "user_c"); !isConflict(err) {
		t.Errorf("expected conflict on stale version, got %v", err)
	}

	// Update against a tenant with no row -> conflict, not create.
	other := tenant + "_other"
	ensureTenant(t, store, other)
	if _, err := store.PutClaudeCodePolicy(ctx, other, 3, true, rules, "user_d"); !isConflict(err) {
		t.Errorf("expected conflict updating missing row, got %v", err)
	}

	// Read-back round trip.
	got, err := store.GetClaudeCodePolicy(ctx, tenant)
	if err != nil || got.Version != 2 || got.UpdatedBy != "user_b" || len(got.Rules) != 0 {
		t.Errorf("readback: err=%v policy=%+v", err, got)
	}
}
