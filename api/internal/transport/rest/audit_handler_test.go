package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// ─── Test doubles ───────────────────────────────────────────

type fakeRepo struct {
	mu    sync.Mutex
	rows  map[string][]*audit.AuditEvent
	lastH map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[string][]*audit.AuditEvent{}, lastH: map[string]string{}}
}

func (r *fakeRepo) AppendChained(ctx context.Context, t string, e *audit.AuditEvent) (*audit.AuditEvent, error) {
	out, err := r.AppendBatchChained(ctx, t, []*audit.AuditEvent{e})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}
func (r *fakeRepo) AppendBatchChained(ctx context.Context, t string, events []*audit.AuditEvent) ([]*audit.AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev, ok := r.lastH[t]
	if !ok {
		prev = audit.GenesisHash
	}
	out := make([]*audit.AuditEvent, 0, len(events))
	for _, e := range events {
		e.TenantID = t
		e.PrevHash = prev
		e.SequenceNumber = int64(len(r.rows[t]) + 1)
		e.Hash = audit.ComputeHash(e)
		r.rows[t] = append(r.rows[t], e)
		prev = e.Hash
		out = append(out, e)
	}
	r.lastH[t] = prev
	return out, nil
}
func (r *fakeRepo) Get(ctx context.Context, t, id string) (*audit.AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.rows[t] {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, apperrors.NotFound("audit_event", id)
}
func (r *fakeRepo) Previous(ctx context.Context, t, id string) (*audit.AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.rows[t] {
		if e.ID == id {
			if i == 0 {
				return nil, nil
			}
			return r.rows[t][i-1], nil
		}
	}
	return nil, apperrors.NotFound("audit_event", id)
}
func (r *fakeRepo) List(ctx context.Context, t string, f audit.Filter) ([]*audit.AuditEvent, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := append([]*audit.AuditEvent(nil), r.rows[t]...)
	return all, len(all), nil
}
func (r *fakeRepo) Walk(ctx context.Context, t string, from, to time.Time, fn func(*audit.AuditEvent) error) (int, error) {
	r.mu.Lock()
	rows := append([]*audit.AuditEvent(nil), r.rows[t]...)
	r.mu.Unlock()
	c := 0
	for _, e := range rows {
		if err := fn(e); err != nil {
			return c, err
		}
		c++
	}
	return c, nil
}
func (r *fakeRepo) CountByAction(ctx context.Context, t string, from, to time.Time) (map[string]int, error) {
	return nil, nil
}
func (r *fakeRepo) CountByActorType(ctx context.Context, t string, from, to time.Time) (map[string]int, error) {
	return nil, nil
}
func (r *fakeRepo) GetBySeq(ctx context.Context, t string, seq int64) (*audit.AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.rows[t] {
		if e.SequenceNumber == seq {
			return e, nil
		}
	}
	return nil, apperrors.NotFound("audit_event", fmt.Sprintf("seq=%d", seq))
}
func (r *fakeRepo) ReanchorFrom(ctx context.Context, t string, fromSeq int64, startPrevHash string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := startPrevHash
	var updated int64
	for _, e := range r.rows[t] {
		if e.SequenceNumber >= fromSeq {
			e.PrevHash = prev
			e.Hash = audit.ComputeHash(e)
			prev = e.Hash
			updated++
		}
	}
	r.lastH[t] = prev
	return updated, nil
}

type fakeKeyStore struct{ keys map[string]*audit.APIKey }

func (k *fakeKeyStore) CreateKey(ctx context.Context, key *audit.APIKey) error {
	k.keys[key.Hash] = key
	return nil
}
func (k *fakeKeyStore) GetKeyByID(ctx context.Context, id string) (*audit.APIKey, error) {
	for _, v := range k.keys {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, apperrors.NotFound("api_key", "")
}
func (k *fakeKeyStore) ListKeys(ctx context.Context, tenantID string) ([]*audit.APIKey, error) {
	var out []*audit.APIKey
	for _, v := range k.keys {
		if v.TenantID == tenantID {
			out = append(out, v)
		}
	}
	return out, nil
}
func (k *fakeKeyStore) RevokeKey(ctx context.Context, t, id string) error {
	for _, v := range k.keys {
		if v.TenantID == t && v.ID == id {
			n := time.Now().UTC()
			v.RevokedAt = &n
			return nil
		}
	}
	return apperrors.NotFound("api_key", id)
}
func (k *fakeKeyStore) RevokeAllKeys(ctx context.Context, t string) error {
	n := time.Now().UTC()
	for _, v := range k.keys {
		if v.TenantID == t && v.RevokedAt == nil {
			v.RevokedAt = &n
		}
	}
	return nil
}

// ─── Harness ────────────────────────────────────────────────

type harness struct {
	srv  *httptest.Server
	repo *fakeRepo
	keys map[string]string // tenantID -> raw key
}

func newHarness(t *testing.T, tenants ...string) *harness {
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
		h.Mount(r)
	})

	return &harness{
		srv:  httptest.NewServer(r),
		repo: repo,
		keys: rawKeys,
	}
}

func (h *harness) close() { h.srv.Close() }

func (h *harness) do(t *testing.T, method, path, key string, body any) *http.Response {
	return h.doWithHeaders(t, method, path, key, body, nil)
}

func (h *harness) doWithHeaders(t *testing.T, method, path, key string, body any, extra map[string]string) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func sampleWire(action string) map[string]any {
	return map[string]any{
		"actor":    map[string]any{"id": "u1", "type": "human", "email": "u@x.com"},
		"action":   action,
		"resource": map[string]any{"id": "doc1", "type": "document", "name": "X"},
		"outcome":  "success",
	}
}

// ─── Tests ──────────────────────────────────────────────────

func TestAPI_RejectsMissingKey(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()

	resp := h.do(t, "GET", "/v1/events", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAPI_RejectsBadKey(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()

	resp := h.do(t, "GET", "/v1/events", "alk_live_doesnotexistxxxxxx", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAPI_LogAndList_RoundTrip(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()
	keyA := h.keys["tenant_a"]

	for i := 0; i < 3; i++ {
		resp := h.do(t, "POST", "/v1/events", keyA, sampleWire("doc.read"))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("log status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	resp := h.do(t, "GET", "/v1/events", keyA, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	var body struct {
		Events []audit.AuditEvent `json:"events"`
		Total  int                `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	if body.Total != 3 {
		t.Fatalf("total = %d, want 3", body.Total)
	}
}

// TestAPI_CrossTenantIsolation is the key compliance guarantee test.
func TestAPI_CrossTenantIsolation(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a", "tenant_b")
	defer h.close()
	keyA, keyB := h.keys["tenant_a"], h.keys["tenant_b"]

	for i := 0; i < 5; i++ {
		_ = h.do(t, "POST", "/v1/events", keyA, sampleWire("a.write")).Body.Close()
		_ = h.do(t, "POST", "/v1/events", keyB, sampleWire("b.write")).Body.Close()
	}

	// Tenant A query must return only A events.
	resp := h.do(t, "GET", "/v1/events", keyA, nil)
	var listA struct {
		Events []audit.AuditEvent `json:"events"`
		Total  int                `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listA)
	_ = resp.Body.Close()
	if listA.Total != 5 {
		t.Fatalf("tenant_a total = %d, want 5", listA.Total)
	}
	for _, e := range listA.Events {
		if e.TenantID != "tenant_a" || e.Action != "a.write" {
			t.Fatalf("leaked event for tenant_a: %+v", e)
		}
	}

	// Tenant B query must return only B events.
	resp = h.do(t, "GET", "/v1/events", keyB, nil)
	var listB struct {
		Events []audit.AuditEvent `json:"events"`
		Total  int                `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listB)
	_ = resp.Body.Close()
	if listB.Total != 5 {
		t.Fatalf("tenant_b total = %d, want 5", listB.Total)
	}
	for _, e := range listB.Events {
		if e.TenantID != "tenant_b" || e.Action != "b.write" {
			t.Fatalf("leaked event for tenant_b: %+v", e)
		}
	}

	// Try to fetch tenant B's first event using tenant A's key.
	bID := listB.Events[0].ID
	resp = h.do(t, "GET", "/v1/events/"+bID, keyA, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-tenant get status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestAPI_Verify_DetectsTamper(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()
	keyA := h.keys["tenant_a"]

	for i := 0; i < 10; i++ {
		_ = h.do(t, "POST", "/v1/events", keyA, sampleWire("doc.read")).Body.Close()
	}

	// Tamper directly on the in-memory store, simulating a malicious DB UPDATE.
	h.repo.rows["tenant_a"][3].Action = "doc.delete"

	resp := h.do(t, "GET", "/v1/verify", keyA, nil)
	var got audit.VerifyResult
	_ = json.NewDecoder(resp.Body).Decode(&got)
	_ = resp.Body.Close()
	if got.Valid {
		t.Fatalf("expected invalid")
	}
	if got.FirstBrokenEventID == "" {
		t.Fatalf("missing first broken event id")
	}
}

func TestAPI_Batch_HashChainIntegrity(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()
	keyA := h.keys["tenant_a"]

	// Build 100 events, send as one batch.
	events := make([]map[string]any, 100)
	for i := range events {
		events[i] = sampleWire(fmt.Sprintf("act.%d", i))
	}
	resp := h.do(t, "POST", "/v1/events/batch", keyA, map[string]any{"events": events})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch status = %d", resp.StatusCode)
	}
	var batchRes audit.BatchResult
	_ = json.NewDecoder(resp.Body).Decode(&batchRes)
	_ = resp.Body.Close()
	if batchRes.Accepted != 100 {
		t.Fatalf("accepted = %d, want 100", batchRes.Accepted)
	}

	// Verify the chain across the batch.
	resp = h.do(t, "GET", "/v1/verify", keyA, nil)
	var got audit.VerifyResult
	_ = json.NewDecoder(resp.Body).Decode(&got)
	_ = resp.Body.Close()
	if !got.Valid {
		t.Fatalf("chain broken across batch: %s @ %s", got.Reason, got.FirstBrokenEventID)
	}
	if got.EventsChecked != 100 {
		t.Fatalf("events checked = %d, want 100", got.EventsChecked)
	}
}

func TestAPI_Filtering(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()
	key := h.keys["tenant_a"]

	for _, action := range []string{"a.b", "a.c", "a.b", "x.y"} {
		_ = h.do(t, "POST", "/v1/events", key, sampleWire(action)).Body.Close()
	}

	// In our fake repo List ignores filters (the Postgres implementation
	// does the actual filtering), so just ensure the endpoint returns 200
	// and includes a page_size echo.
	resp := h.do(t, "GET", "/v1/events?action=a.b&page_size=10", key, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	_ = resp.Body.Close()
	if body["page_size"].(float64) != 10 {
		t.Fatalf("page_size echo wrong: %v", body["page_size"])
	}
}

// Smoke test: malformed Authorization header returns 401 with WWW-Authenticate.
func TestAPI_Auth_Header(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "tenant_a")
	defer h.close()
	req, _ := http.NewRequest("GET", h.srv.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "NotBearer xyz")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("missing WWW-Authenticate header")
	}
}

// fakeOutbox implements audit.BatchEnqueuer and audit.ReceiptStore for handler tests.
type fakeOutbox struct {
	mu       sync.Mutex
	receipts map[string]*audit.AsyncReceipt
}

func (f *fakeOutbox) Enqueue(_ context.Context, tenantID, receiptID string, _ []*audit.AuditEvent, validation *audit.BatchResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.receipts == nil {
		f.receipts = map[string]*audit.AsyncReceipt{}
	}
	f.receipts[receiptID] = &audit.AsyncReceipt{
		ReceiptID:   receiptID,
		Status:      "queued",
		BatchResult: *validation,
	}
	return nil
}

func (f *fakeOutbox) GetReceipt(_ context.Context, _, receiptID string) (*audit.AsyncReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.receipts[receiptID]; ok {
		return r, nil
	}
	return nil, apperrors.NotFound("ingest_receipt", receiptID)
}

func newAsyncHarness(t *testing.T) (*harness, *fakeOutbox) {
	t.Helper()
	repo := newFakeRepo()
	keyStore := &fakeKeyStore{keys: map[string]*audit.APIKey{}}
	rawToken, hashStr, _ := auth.GenerateKey("akid_async")
	_ = keyStore.CreateKey(context.Background(), &audit.APIKey{
		ID: "akid_async", TenantID: "tenant_a", Hash: hashStr, Scope: audit.ScopeWrite,
	})
	ob := &fakeOutbox{}
	svc := audit.NewServiceWithOptions(repo, zerolog.Nop(), false, audit.ServiceOptions{
		Enqueuer: ob, Receipts: ob, AsyncDefault: true,
	})
	h := NewAuditHandler(svc, nil, nil, zerolog.Nop(), AuditHandlerConfig{AsyncDefault: true})
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.APIKey(keyStore))
		h.Mount(r)
	})
	return &harness{srv: httptest.NewServer(r), repo: repo, keys: map[string]string{"tenant_a": rawToken}}, ob
}

func TestAPI_AsyncBatch_Returns202(t *testing.T) {
	t.Parallel()
	h, ob := newAsyncHarness(t)
	defer h.close()
	key := h.keys["tenant_a"]

	resp := h.do(t, "POST", "/v1/events/batch", key, map[string]any{
		"events": []map[string]any{sampleWire("async.batch")},
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	var receipt audit.AsyncReceipt
	if err := json.NewDecoder(resp.Body).Decode(&receipt); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	if receipt.Status != "queued" || receipt.ReceiptID == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	ob.mu.Lock()
	stored := ob.receipts[receipt.ReceiptID]
	ob.mu.Unlock()
	if stored == nil {
		t.Fatalf("receipt not enqueued")
	}
}

func TestAPI_SyncBatch_Returns200(t *testing.T) {
	t.Parallel()
	h, _ := newAsyncHarness(t)
	defer h.close()
	key := h.keys["tenant_a"]

	resp := h.doWithHeaders(t, "POST", "/v1/events/batch", key, map[string]any{
		"events": []map[string]any{sampleWire("sync.batch")},
	}, map[string]string{"X-Fact0-Sync": "true"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var res audit.BatchResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	_ = resp.Body.Close()
	if res.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", res.Accepted)
	}
}

func TestAPI_GetReceipt(t *testing.T) {
	t.Parallel()
	h, ob := newAsyncHarness(t)
	defer h.close()
	key := h.keys["tenant_a"]

	resp := h.do(t, "POST", "/v1/events/batch", key, map[string]any{
		"events": []map[string]any{sampleWire("receipt.test")},
	})
	var receipt audit.AsyncReceipt
	_ = json.NewDecoder(resp.Body).Decode(&receipt)
	_ = resp.Body.Close()

	resp = h.do(t, "GET", "/v1/receipts/"+receipt.ReceiptID, key, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get receipt status = %d", resp.StatusCode)
	}
	var got audit.AsyncReceipt
	_ = json.NewDecoder(resp.Body).Decode(&got)
	_ = resp.Body.Close()
	if got.ReceiptID != receipt.ReceiptID || got.Status != "queued" {
		t.Fatalf("unexpected receipt: %+v", got)
	}
	_ = ob
}
