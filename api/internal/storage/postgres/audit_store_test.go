package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

func testAuditStore(t *testing.T) (*postgres.AuditStore, func()) {
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
	store := postgres.NewAuditStore(pool, pool, zerolog.Nop())
	return store, func() { pool.Close() }
}

func sampleEvent(id, action string) *audit.AuditEvent {
	return &audit.AuditEvent{
		ID: id,
		Actor: audit.Actor{
			ID:   "actor-1",
			Type: audit.ActorAgent,
		},
		Action: action,
		Resource: audit.Resource{
			ID:   "res-1",
			Type: "doc",
		},
		Outcome:   audit.OutcomeSuccess,
		Metadata:  map[string]interface{}{},
		Timestamp: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func ensureTenant(t *testing.T, store *postgres.AuditStore, id string) {
	t.Helper()
	err := store.CreateTenant(context.Background(), &audit.Tenant{ID: id, Name: "test"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
}

func TestAppendBatchChained_BulkChainIntegrity(t *testing.T) {
	store, cleanup := testAuditStore(t)
	defer cleanup()
	ctx := context.Background()
	tenantID := "tnt_test_bulk_" + time.Now().Format("150405")
	ensureTenant(t, store, tenantID)

	events := make([]*audit.AuditEvent, 100)
	for i := range events {
		events[i] = sampleEvent(audit.NewEventID(), "bulk.act")
	}

	written, err := store.AppendBatchChained(ctx, tenantID, events)
	if err != nil {
		t.Fatalf("append batch: %v", err)
	}
	if len(written) != 100 {
		t.Fatalf("written = %d, want 100", len(written))
	}

	prev := audit.GenesisHash
	for i, e := range written {
		if e.PrevHash != prev {
			t.Fatalf("event %d prev_hash mismatch", i)
		}
		if audit.ComputeHash(e) != e.Hash {
			t.Fatalf("event %d hash mismatch", i)
		}
		prev = e.Hash
	}
}

func TestAppendBatchChained_IdempotentRetry(t *testing.T) {
	store, cleanup := testAuditStore(t)
	defer cleanup()
	ctx := context.Background()
	tenantID := "tnt_test_idem_" + time.Now().Format("150405")
	ensureTenant(t, store, tenantID)

	e := sampleEvent(audit.NewEventID(), "idem.act")
	first, err := store.AppendBatchChained(ctx, tenantID, []*audit.AuditEvent{e})
	if err != nil {
		t.Fatalf("first append: %v", err)
	}

	dup := sampleEvent(e.ID, "idem.act")
	dup.Timestamp = e.Timestamp
	second, err := store.AppendBatchChained(ctx, tenantID, []*audit.AuditEvent{dup})
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("second len = %d", len(second))
	}
	if second[0].SequenceNumber != first[0].SequenceNumber {
		t.Fatalf("sequence changed on retry")
	}
}
