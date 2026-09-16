package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

func testStore(t *testing.T) (*postgres.Store, *postgres.AuditStore, func()) {
	t.Helper()
	dsn := os.Getenv("FACT0_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("FACT0_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := postgres.ConnectPool(ctx, dsn, postgres.PoolOptions{MaxConns: 5, MinConns: 1})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	store := postgres.NewStore(pool, zerolog.Nop())
	auditStore := postgres.NewAuditStore(pool, pool, zerolog.Nop())
	return store, auditStore, func() { pool.Close() }
}

func TestListExecutions_TenantIsolation(t *testing.T) {
	store, auditStore, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	tenantA := "tnt_iso_a_" + suffix
	tenantB := "tnt_iso_b_" + suffix
	for _, id := range []string{tenantA, tenantB} {
		if err := auditStore.CreateTenant(ctx, &audit.Tenant{ID: id, Name: id}); err != nil {
			t.Fatalf("create tenant %s: %v", id, err)
		}
	}

	execA := &execution.Execution{
		ID:        "EXEC_test_a_" + suffix,
		AgentID:   "agent-a",
		AgentName: "AgentA",
		Status:    execution.StatusCompleted,
		Trigger:   "test",
		StartedAt: time.Now().UTC(),
		Metadata:  map[string]string{},
	}
	execB := &execution.Execution{
		ID:        "EXEC_test_b_" + suffix,
		AgentID:   "agent-b",
		AgentName: "AgentB",
		Status:    execution.StatusCompleted,
		Trigger:   "test",
		StartedAt: time.Now().UTC(),
		Metadata:  map[string]string{},
	}

	if err := store.InsertExecution(ctx, tenantA, execA); err != nil {
		t.Fatalf("insert A: %v", err)
	}
	if err := store.InsertExecution(ctx, tenantB, execB); err != nil {
		t.Fatalf("insert B: %v", err)
	}

	listA, totalA, err := store.ListExecutions(ctx, tenantA, "", "", 50, 0)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if totalA < 1 {
		t.Fatalf("tenant A total = %d, want >= 1", totalA)
	}
	for _, e := range listA {
		if e.ID == execB.ID {
			t.Fatalf("tenant A list leaked exec B")
		}
	}

	_, err = store.GetExecution(ctx, tenantB, execA.ID)
	if err == nil {
		t.Fatal("tenant B read exec A: expected not found")
	}
}
