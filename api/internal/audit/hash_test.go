package audit

import (
	"strings"
	"testing"
	"time"
)

func TestComputeHash_Deterministic(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 15, 10, 23, 44, 123_000_000, time.UTC)
	e := &AuditEvent{
		ID:        "evt_01HX3K",
		Timestamp: ts,
		Actor:     Actor{ID: "user_123", Type: ActorHuman, Email: "a@b.com"},
		Action:    "document.delete",
		Resource:  Resource{ID: "doc_456", Type: "document", Name: "Q3 Report"},
		Outcome:   OutcomeSuccess,
		PrevHash:  GenesisHash,
	}

	h1 := ComputeHash(e)
	h2 := ComputeHash(e)
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, HashPrefix) {
		t.Fatalf("hash missing prefix: %q", h1)
	}
	if got, want := len(h1), len(HashPrefix)+64; got != want {
		t.Fatalf("hash length = %d, want %d (%q)", got, want, h1)
	}
}

func TestComputeHash_ChangesOnAnyField(t *testing.T) {
	t.Parallel()
	base := &AuditEvent{
		ID:        "evt_01",
		Timestamp: time.Unix(0, 0).UTC(),
		Actor:     Actor{ID: "u", Type: ActorHuman},
		Action:    "x.y",
		Resource:  Resource{ID: "r", Type: "t"},
		Outcome:   OutcomeSuccess,
		PrevHash:  GenesisHash,
	}
	baseHash := ComputeHash(base)

	mutations := map[string]func(*AuditEvent){
		"id":              func(e *AuditEvent) { e.ID = "evt_02" },
		"timestamp":       func(e *AuditEvent) { e.Timestamp = e.Timestamp.Add(time.Microsecond) },
		"tenant_id":       func(e *AuditEvent) { e.TenantID = "tenant-2" },
		"sequence_number": func(e *AuditEvent) { e.SequenceNumber = 42 },
		"actor.type":      func(e *AuditEvent) { e.Actor.Type = ActorAgent },
		"resource.type":   func(e *AuditEvent) { e.Resource.Type = "other" },
		"actor.id":        func(e *AuditEvent) { e.Actor.ID = "u2" },
		"actor.email":     func(e *AuditEvent) { e.Actor.Email = "new@email.com" },
		"action":          func(e *AuditEvent) { e.Action = "x.z" },
		"resource.id":     func(e *AuditEvent) { e.Resource.ID = "r2" },
		"resource.name":   func(e *AuditEvent) { e.Resource.Name = "new_name" },
		"outcome":         func(e *AuditEvent) { e.Outcome = OutcomeFailure },
		"prev_hash":       func(e *AuditEvent) { e.PrevHash = "sha256:deadbeef" },
		"metadata":        func(e *AuditEvent) { e.Metadata = map[string]any{"key": "value"} },
	}

	for name, mut := range mutations {
		t.Run(name, func(t *testing.T) {
			cp := *base
			mut(&cp)
			if got := ComputeHash(&cp); got == baseHash {
				t.Fatalf("hash unchanged after mutating %s: %q", name, got)
			}
		})
	}
}

func TestComputeHash_GoldenVector(t *testing.T) {
	t.Parallel()
	// Pinning a known input so any accidental change to the hashing
	// recipe (field order, separator, prefix) surfaces in CI.
	e := &AuditEvent{
		ID:        "evt_test",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Actor:     Actor{ID: "user_1", Type: ActorHuman},
		Action:    "doc.read",
		Resource:  Resource{ID: "doc_1", Type: "document"},
		Outcome:   OutcomeSuccess,
		PrevHash:  GenesisHash,
	}
	const want = "sha256:v2:7d2ffbff9da42749041c28b7bf489b253a6e81a373c075316027c9457ecea31f"
	if got := ComputeHash(e); got != want {
		t.Fatalf("golden vector drift:\n  got:  %s\n  want: %s", got, want)
	}
}
