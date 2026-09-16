package audit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeRepo is a single-tenant-aware in-memory implementation of Repository
// used only by these unit tests. It mimics the Postgres store's
// chain-extension semantics so service-level tests do not need a database.
type fakeRepo struct {
	mu    sync.Mutex
	rows  map[string][]*AuditEvent // keyed by tenant
	lastH map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		rows:  map[string][]*AuditEvent{},
		lastH: map[string]string{},
	}
}

func (r *fakeRepo) AppendChained(ctx context.Context, tenantID string, e *AuditEvent) (*AuditEvent, error) {
	out, err := r.AppendBatchChained(ctx, tenantID, []*AuditEvent{e})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

func (r *fakeRepo) AppendBatchChained(ctx context.Context, tenantID string, events []*AuditEvent) ([]*AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev, ok := r.lastH[tenantID]
	if !ok {
		prev = GenesisHash
	}
	out := make([]*AuditEvent, 0, len(events))
	for _, e := range events {
		e.TenantID = tenantID
		e.PrevHash = prev
		e.SequenceNumber = int64(len(r.rows[tenantID]) + 1)
		e.Hash = ComputeHash(e)
		r.rows[tenantID] = append(r.rows[tenantID], e)
		prev = e.Hash
		out = append(out, e)
	}
	r.lastH[tenantID] = prev
	return out, nil
}

func (r *fakeRepo) Get(ctx context.Context, tenantID, id string) (*AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.rows[tenantID] {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (r *fakeRepo) Previous(ctx context.Context, tenantID, id string) (*AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.rows[tenantID] {
		if e.ID == id {
			if i == 0 {
				return nil, nil
			}
			return r.rows[tenantID][i-1], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (r *fakeRepo) List(ctx context.Context, tenantID string, f Filter) ([]*AuditEvent, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.rows[tenantID]
	return all, len(all), nil
}

func (r *fakeRepo) Walk(ctx context.Context, tenantID string, from, to time.Time, fn func(*AuditEvent) error) (int, error) {
	r.mu.Lock()
	rows := append([]*AuditEvent(nil), r.rows[tenantID]...)
	r.mu.Unlock()
	count := 0
	for _, e := range rows {
		if !from.IsZero() && e.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && e.Timestamp.After(to) {
			continue
		}
		if err := fn(e); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (r *fakeRepo) CountByAction(ctx context.Context, tenantID string, from, to time.Time) (map[string]int, error) {
	return nil, nil
}
func (r *fakeRepo) CountByActorType(ctx context.Context, tenantID string, from, to time.Time) (map[string]int, error) {
	return nil, nil
}
func (r *fakeRepo) GetBySeq(ctx context.Context, tenantID string, seq int64) (*AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.rows[tenantID] {
		if e.SequenceNumber == seq {
			return e, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (r *fakeRepo) ReanchorFrom(ctx context.Context, tenantID string, fromSeq int64, startPrevHash string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := startPrevHash
	var updated int64
	for _, e := range r.rows[tenantID] {
		if e.SequenceNumber >= fromSeq {
			e.PrevHash = prev
			e.Hash = ComputeHash(e)
			prev = e.Hash
			updated++
		}
	}
	r.lastH[tenantID] = prev
	return updated, nil
}

func newTestSvc() (*Service, *fakeRepo) {
	r := newFakeRepo()
	base := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	var seq int
	svc := NewServiceWithOptions(r, zerolog.Nop(), false, ServiceOptions{
		Now: func() time.Time {
			seq++
			return base.Add(time.Duration(seq) * time.Microsecond)
		},
	})
	return svc, r
}

func sampleEvent(action string) *AuditEvent {
	return &AuditEvent{
		Actor:    Actor{ID: "user_1", Type: ActorHuman, Email: "u@x.com"},
		Action:   action,
		Resource: Resource{ID: "doc_1", Type: "document", Name: "x"},
		Outcome:  OutcomeSuccess,
	}
}

func TestService_LogChainsCorrectly(t *testing.T) {
	t.Parallel()
	svc, repo := newTestSvc()
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		if _, err := svc.Log(ctx, "tenant_a", sampleEvent("doc.read")); err != nil {
			t.Fatalf("log %d: %v", i, err)
		}
	}

	// Verify clean chain
	res, err := svc.Verify(ctx, "tenant_a", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid chain, got broken at %s: %s", res.FirstBrokenEventID, res.Reason)
	}
	if res.EventsChecked != 100 {
		t.Fatalf("EventsChecked = %d, want 100", res.EventsChecked)
	}
	if res.RootHash == "" {
		t.Fatalf("RootHash empty")
	}
	_ = repo
}

func TestService_VerifyDetectsTamperedHash(t *testing.T) {
	t.Parallel()
	svc, repo := newTestSvc()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if _, err := svc.Log(ctx, "tenant_b", sampleEvent("doc.read")); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	// Tamper with row 5 (simulating a malicious DB UPDATE).
	tampered := repo.rows["tenant_b"][5]
	tampered.Action = "doc.delete" // hash now no longer matches stored hash

	res, err := svc.Verify(ctx, "tenant_b", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.EventsChecked != 10 {
		t.Fatalf("EventsChecked = %d, want 10", res.EventsChecked)
	}
	if res.Valid {
		t.Fatalf("expected invalid chain")
	}
	if res.FirstBrokenEventID != tampered.ID {
		t.Fatalf("FirstBrokenEventID = %q, want %q", res.FirstBrokenEventID, tampered.ID)
	}
}

func TestService_VerifyDetectsBrokenPrevHash(t *testing.T) {
	t.Parallel()
	svc, repo := newTestSvc()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if _, err := svc.Log(ctx, "tenant_c", sampleEvent("doc.read")); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	// Corrupt prev_hash on row 3, then re-derive its own hash so the
	// per-row check passes - the chain check (prev_hash continuity) must
	// still flag it.
	tampered := repo.rows["tenant_c"][3]
	tampered.PrevHash = "sha256:" + "ab" + tampered.PrevHash[9:]
	tampered.Hash = ComputeHash(tampered)

	res, err := svc.Verify(ctx, "tenant_c", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.EventsChecked != 10 {
		t.Fatalf("EventsChecked = %d, want 10 (verify window excluded events?)", res.EventsChecked)
	}
	if res.Valid {
		t.Fatalf("expected invalid chain")
	}
	if res.FirstBrokenEventID != tampered.ID {
		t.Fatalf("FirstBrokenEventID = %q, want %q", res.FirstBrokenEventID, tampered.ID)
	}
}

func TestService_LogBatchChains(t *testing.T) {
	t.Parallel()
	svc, _ := newTestSvc()
	ctx := context.Background()

	events := make([]*AuditEvent, 100)
	for i := range events {
		events[i] = sampleEvent("doc.read")
	}
	res, err := svc.LogBatch(ctx, "tenant_d", events)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if res.Accepted != 100 || res.Rejected != 0 {
		t.Fatalf("accepted=%d rejected=%d", res.Accepted, res.Rejected)
	}

	verify, err := svc.Verify(ctx, "tenant_d", time.Time{}, time.Time{})
	if err != nil || !verify.Valid {
		t.Fatalf("chain broken across batch: valid=%v reason=%s", verify.Valid, verify.Reason)
	}
}

func TestService_LogBatchRejectsInvalid(t *testing.T) {
	t.Parallel()
	svc, _ := newTestSvc()
	ctx := context.Background()

	events := []*AuditEvent{
		sampleEvent("doc.read"),
		{}, // invalid (empty actor)
		sampleEvent("doc.delete"),
	}
	res, err := svc.LogBatch(ctx, "tenant_e", events)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if res.Accepted != 2 || res.Rejected != 1 {
		t.Fatalf("accepted=%d rejected=%d", res.Accepted, res.Rejected)
	}
	if len(res.Errors) != 1 || res.Errors[0].Index != 1 {
		t.Fatalf("expected error at index 1, got %+v", res.Errors)
	}
}

func TestService_VerifyOne(t *testing.T) {
	t.Parallel()
	svc, repo := newTestSvc()
	ctx := context.Background()

	got, err := svc.Log(ctx, "t", sampleEvent("a.b"))
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	res, err := svc.VerifyOne(ctx, "t", got.ID)
	if err != nil || !res.Valid {
		t.Fatalf("expected valid, got %+v err=%v", res, err)
	}

	repo.rows["t"][0].Outcome = OutcomeFailure // tamper
	res, err = svc.VerifyOne(ctx, "t", got.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected invalid after tamper")
	}
}

// Sanity check: Validate rejects bad outcomes.
func TestValidate_BadFields(t *testing.T) {
	t.Parallel()
	cases := []*AuditEvent{
		{},
		{Actor: Actor{ID: "x", Type: "weird"}},
		{Actor: Actor{ID: "x", Type: ActorHuman}, Action: "", Resource: Resource{ID: "r", Type: "t"}, Outcome: OutcomeSuccess},
		{Actor: Actor{ID: "x", Type: ActorHuman}, Action: "a.b", Resource: Resource{ID: "", Type: "t"}, Outcome: OutcomeSuccess},
	}
	for i, c := range cases {
		if err := Validate(c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	if err := Validate(nil); err == nil || !errors.Is(err, err) {
		t.Errorf("nil case should error")
	}
}
