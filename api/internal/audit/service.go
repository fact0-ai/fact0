package audit

import (
	"context"
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/rs/zerolog"

	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
	"github.com/fact0-ai/fact0/internal/redaction"
)

// MaxBatchSize caps the number of events accepted in a single batch
// ingest call (matches the brief's 1000-event maximum).
const MaxBatchSize = 1000

// Service is the audit-log application service. It owns chain
// validation, fanout to subscribers, and the verification recipe.
type Service struct {
	repo         Repository
	logger       zerolog.Logger
	now          func() time.Time
	redactOn     bool
	subsMu       sync.RWMutex
	subs         []chan *AuditEvent
	enqueuer     BatchEnqueuer
	receipts     ReceiptStore
	asyncDefault bool
	signingKey   ed25519.PrivateKey
}

// BatchEnqueuer queues validated events for async chain commit.
type BatchEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, receiptID string, events []*AuditEvent, validation *BatchResult) error
}

// ReceiptStore reads async ingest receipts.
type ReceiptStore interface {
	GetReceipt(ctx context.Context, tenantID, receiptID string) (*AsyncReceipt, error)
}

// ServiceOptions configures optional async ingest behavior.
type ServiceOptions struct {
	Enqueuer     BatchEnqueuer
	Receipts     ReceiptStore
	AsyncDefault bool
	// Now overrides the clock used for timestamps and verify windows (tests).
	Now func() time.Time
	// SigningKey is used to sign EvidencePack ZIPs.
	SigningKey ed25519.PrivateKey
}

// farFuture is the supported upper bound for open-ended audit scans and
// exports, including already-recorded events captured by clocks ahead of ours.
var farFuture = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)

// historyStart is the earliest supported capture time and default audit range.
var historyStart = time.Unix(0, 0).UTC()

// NewService constructs a Service. When redactionEnabled is true,
// every event's metadata is scrubbed via redaction.RedactJSONMap before
// it enters the chain - critical that redaction runs PRE-hash, so chain
// verification keeps working on stored events.
func NewService(repo Repository, logger zerolog.Logger, redactionEnabled bool) *Service {
	return NewServiceWithOptions(repo, logger, redactionEnabled, ServiceOptions{})
}

// NewServiceWithOptions constructs a Service with async ingest support.
func NewServiceWithOptions(repo Repository, logger zerolog.Logger, redactionEnabled bool, opts ServiceOptions) *Service {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		repo:         repo,
		logger:       logger.With().Str("module", "audit").Logger(),
		now:          now,
		redactOn:     redactionEnabled,
		enqueuer:     opts.Enqueuer,
		receipts:     opts.Receipts,
		asyncDefault: opts.AsyncDefault,
		signingKey:   opts.SigningKey,
	}
}

// AsyncDefault reports whether batch ingest defaults to async mode.
func (s *Service) AsyncDefault() bool { return s.asyncDefault }

// Log validates and persists a single event, returning the canonical row
// (with id, timestamp, prev_hash, hash, sequence_number populated).
func (s *Service) Log(ctx context.Context, tenantID string, in *AuditEvent) (*AuditEvent, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenant_id required")
	}
	if err := Validate(in); err != nil {
		metrics.IngestRejected.WithLabelValues("audit_event", "validation").Inc()
		return nil, err
	}
	s.applyDefaults(in)
	s.applyRedaction(in)

	out, err := s.repo.AppendChained(ctx, tenantID, in)
	if err != nil {
		metrics.IngestRejected.WithLabelValues("audit_event", "storage").Inc()
		return nil, err
	}
	metrics.IngestAccepted.WithLabelValues("audit_event").Inc()
	s.fanout(out)
	return out, nil
}

// LogBatch validates and persists up to MaxBatchSize events. Returns the
// per-event acceptance status; chain ordering follows slice order.
type BatchResult struct {
	Accepted int          `json:"accepted"`
	Rejected int          `json:"rejected"`
	IDs      []string     `json:"ids"`
	Errors   []BatchError `json:"errors,omitempty"`
}

// BatchError reports a single per-event validation failure. Storage errors
// abort the entire batch transaction and surface as a returned error.
type BatchError struct {
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AsyncReceipt is returned for async batch ingest and receipt polling.
type AsyncReceipt struct {
	ReceiptID string `json:"receipt_id"`
	Status    string `json:"status"`
	BatchResult
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CommittedAt  *time.Time `json:"committed_at,omitempty"`
}

// LogBatch accepts a slice of events and writes them synchronously in one transaction.
func (s *Service) LogBatch(ctx context.Context, tenantID string, in []*AuditEvent) (*BatchResult, error) {
	res, valid, err := s.prepareBatch(tenantID, in)
	if err != nil {
		return nil, err
	}
	if len(valid) == 0 {
		return res, nil
	}
	written, err := s.CommitBatch(ctx, tenantID, valid)
	if err != nil {
		return nil, err
	}
	for _, e := range written {
		res.IDs = append(res.IDs, e.ID)
		s.fanout(e)
	}
	res.Accepted = len(written)
	return res, nil
}

// EnqueueBatch validates events and queues them for async chain commit.
func (s *Service) EnqueueBatch(ctx context.Context, tenantID string, in []*AuditEvent) (*AsyncReceipt, error) {
	if s.enqueuer == nil {
		return nil, apperrors.Internal("async ingest not configured", nil)
	}
	res, valid, err := s.prepareBatch(tenantID, in)
	if err != nil {
		return nil, err
	}
	receiptID := NewReceiptID()
	res.Accepted = len(valid)
	if len(valid) == 0 {
		return &AsyncReceipt{
			ReceiptID:   receiptID,
			Status:      "committed",
			BatchResult: *res,
		}, nil
	}
	if err := s.enqueuer.Enqueue(ctx, tenantID, receiptID, valid, res); err != nil {
		return nil, err
	}
	return &AsyncReceipt{
		ReceiptID:   receiptID,
		Status:      "queued",
		BatchResult: *res,
	}, nil
}

// GetReceipt returns the status of an async ingest receipt.
func (s *Service) GetReceipt(ctx context.Context, tenantID, receiptID string) (*AsyncReceipt, error) {
	if s.receipts == nil {
		return nil, apperrors.NotFound("ingest_receipt", receiptID)
	}
	r, err := s.receipts.GetReceipt(ctx, tenantID, receiptID)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// CommitBatch persists pre-validated events without fanout (used by outbox worker).
func (s *Service) CommitBatch(ctx context.Context, tenantID string, events []*AuditEvent) ([]*AuditEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	written, err := s.repo.AppendBatchChained(ctx, tenantID, events)
	if err != nil {
		metrics.IngestRejected.WithLabelValues("audit_event", "storage").Add(float64(len(events)))
		return nil, err
	}
	metrics.IngestAccepted.WithLabelValues("audit_event").Add(float64(len(written)))
	return written, nil
}

// FanoutEvents notifies live subscribers.
func (s *Service) FanoutEvents(events []*AuditEvent) {
	for _, e := range events {
		s.fanout(e)
	}
}

func (s *Service) prepareBatch(tenantID string, in []*AuditEvent) (*BatchResult, []*AuditEvent, error) {
	if tenantID == "" {
		return nil, nil, apperrors.InvalidInput("tenant_id required")
	}
	if len(in) == 0 {
		return &BatchResult{}, nil, nil
	}
	if len(in) > MaxBatchSize {
		return nil, nil, apperrors.InvalidInput("batch exceeds maximum of 1000 events")
	}

	res := &BatchResult{}
	valid := make([]*AuditEvent, 0, len(in))
	for i, e := range in {
		if err := Validate(e); err != nil {
			metrics.IngestRejected.WithLabelValues("audit_event", "validation").Inc()
			res.Rejected++
			res.Errors = append(res.Errors, BatchError{
				Index:   i,
				Code:    "VALIDATION_ERROR",
				Message: err.Error(),
			})
			continue
		}
		s.applyDefaults(e)
		s.applyRedaction(e)
		valid = append(valid, e)
	}
	return res, valid, nil
}

// applyDefaults fills in id and timestamp if the caller omitted them.
// Timestamps are truncated to microsecond precision to match PostgreSQL
// TIMESTAMPTZ storage, ensuring ComputeHash at write-time and verify-time
// produce identical results (time.Now() can carry sub-microsecond nanoseconds
// that PostgreSQL silently drops).
func (s *Service) applyDefaults(e *AuditEvent) {
	if e.ID == "" {
		e.ID = NewEventID()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = s.now().Truncate(time.Microsecond)
	} else {
		e.Timestamp = e.Timestamp.Truncate(time.Microsecond)
	}
	if e.Metadata == nil {
		e.Metadata = map[string]interface{}{}
	}
}

// applyRedaction scrubs the event's metadata. Runs PRE-hash so the
// chain stays verifiable. Actor / Resource columns are intentionally
// not scrubbed: those identify what was acted on and by whom, which is
// the whole point of an audit log. Free-form payload lives in Metadata.
func (s *Service) applyRedaction(e *AuditEvent) {
	if !s.redactOn {
		return
	}
	if redaction.RedactJSONMap(e.Metadata) {
		// Mark so the UI can show a "Redacted" badge on this row.
		e.Metadata["redacted"] = true
	}
}

// Get returns a single event scoped to the resolved tenant.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*AuditEvent, error) {
	return s.repo.Get(ctx, tenantID, id)
}

// List returns a filtered, paginated page of events.
func (s *Service) List(ctx context.Context, tenantID string, f Filter) ([]*AuditEvent, int, error) {
	return s.repo.List(ctx, tenantID, f)
}

// Verify recomputes the SHA-256 chain across [from,to] for the resolved
// tenant. Returns Valid=false and FirstBrokenEventID on the first hash
// mismatch (in chain order).
func (s *Service) Verify(ctx context.Context, tenantID string, from, to time.Time) (*VerifyResult, error) {
	if from.IsZero() {
		from = historyStart
	}
	if to.IsZero() {
		to = farFuture
	}

	res := &VerifyResult{
		Valid:    true,
		TenantID: tenantID,
		From:     from,
		To:       to,
	}

	count, err := s.repo.Walk(ctx, tenantID, from, to, func(e *AuditEvent) error {
		if !res.Valid {
			return nil
		}
		// Event timestamps may arrive out of chain order. A date-window scan
		// can skip intermediate sequence numbers: compare the actual preceding
		// chain record, never the preceding filtered result.
		prev, err := s.repo.Previous(ctx, tenantID, e.ID)
		if err != nil {
			return err
		}
		expectedPrev := GenesisHash
		if prev != nil {
			expectedPrev = prev.Hash
		}
		if e.PrevHash != expectedPrev {
			res.Valid = false
			res.FirstBrokenEventID = e.ID
			res.Reason = "prev_hash mismatch"
			return nil
		}
		expected := ComputeHash(e)
		if expected != e.Hash {
			res.Valid = false
			res.FirstBrokenEventID = e.ID
			res.Reason = "hash mismatch"
			return nil
		}
		res.RootHash = e.Hash
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.EventsChecked = count
	return res, nil
}

// VerifyDeep walks the entire chain across [from,to] and collects EVERY event
// with a hash or prev_hash mismatch instead of stopping at the first one.
// Use this when you need a full picture of chain health, e.g. for compliance
// PDFs or before a "re-anchor all" sweep.
func (s *Service) VerifyDeep(ctx context.Context, tenantID string, from, to time.Time) (*VerifyResult, error) {
	if from.IsZero() {
		from = historyStart
	}
	if to.IsZero() {
		to = farFuture
	}

	res := &VerifyResult{
		Valid:    true,
		TenantID: tenantID,
		From:     from,
		To:       to,
	}

	count, err := s.repo.Walk(ctx, tenantID, from, to, func(e *AuditEvent) error {
		broken := false
		reason := ""
		prev, err := s.repo.Previous(ctx, tenantID, e.ID)
		if err != nil {
			return err
		}
		expectedPrev := GenesisHash
		if prev != nil {
			expectedPrev = prev.Hash
		}
		if e.PrevHash != expectedPrev {
			broken = true
			reason = "prev_hash mismatch"
		}
		// Independent hash check (stored fingerprint must match recomputed)
		if ComputeHash(e) != e.Hash {
			broken = true
			reason = "hash mismatch" // override: this is the more actionable signal
		}

		if broken {
			res.Valid = false
			res.AllBrokenEvents = append(res.AllBrokenEvents, BrokenEvent{
				EventID:        e.ID,
				SequenceNumber: e.SequenceNumber,
				Timestamp:      e.Timestamp,
				Action:         e.Action,
				ActorID:        e.Actor.ID,
				Reason:         reason,
			})
			// Continue walking from this event's stored hash so we can
			// detect independent breaks further down the chain.
		} else {
			res.RootHash = e.Hash
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	res.EventsChecked = count
	res.BrokenCount = len(res.AllBrokenEvents)
	if len(res.AllBrokenEvents) > 0 {
		res.FirstBrokenEventID = res.AllBrokenEvents[0].EventID
		res.Reason = res.AllBrokenEvents[0].Reason
	}
	return res, nil
}

// ReanchorAll is a one-shot sweep that finds the globally earliest chain
// break and reanchors everything forward in a single operation. It is
// equivalent to clicking "Re-anchor chain" starting from the very first
// broken event across the full history, without requiring the caller to
// know the event ID. A single "chain.reanchored" meta-event is written.
func (s *Service) ReanchorAll(ctx context.Context, tenantID, reason string) (*AuditEvent, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenant_id required")
	}
	if reason == "" {
		return nil, apperrors.InvalidInput("reason required")
	}

	// Find the globally first broken event.
	v, err := s.Verify(ctx, tenantID, time.Time{}, time.Time{})
	if err != nil {
		return nil, err
	}
	if v.Valid {
		// Nothing to do - return a synthetic no-op result.
		return nil, apperrors.InvalidInput("chain is already intact - nothing to re-anchor")
	}

	return s.ReanchorChain(ctx, tenantID, v.FirstBrokenEventID, reason)
}

// VerifyOne re-derives an event's hash and compares to the stored value.
func (s *Service) VerifyOne(ctx context.Context, tenantID, id string) (*VerifyResult, error) {
	e, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	res := &VerifyResult{
		TenantID:      tenantID,
		EventsChecked: 1,
		Valid:         true,
		From:          e.Timestamp,
		To:            e.Timestamp,
		RootHash:      e.Hash,
	}

	prev, err := s.repo.Previous(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	expectedPrev := GenesisHash
	if prev != nil {
		expectedPrev = prev.Hash
	}
	if e.PrevHash != expectedPrev {
		res.Valid = false
		res.FirstBrokenEventID = e.ID
		res.Reason = "prev_hash mismatch"
		return res, nil
	}
	if ComputeHash(e) != e.Hash {
		res.Valid = false
		res.FirstBrokenEventID = e.ID
		res.Reason = "hash mismatch"
	}
	return res, nil
}

// ReanchorChain recomputes hash/prev_hash for every event in the tenant
// chain starting from the event identified by firstBrokenEventID, then
// appends a special "chain.reanchored" meta-event documenting the reason.
//
// This is a destructive operation on the tamper-evidence property: it
// should only be used when the break is caused by a known software bug
// (e.g. a migration accidentally touching a hash-critical column), not
// by external tampering. The meta-event written at the end becomes the
// new chain head, forming an immutable record that re-anchoring occurred.
func (s *Service) ReanchorChain(ctx context.Context, tenantID, firstBrokenEventID, reason string) (*AuditEvent, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenant_id required")
	}
	if firstBrokenEventID == "" {
		return nil, apperrors.InvalidInput("first_broken_event_id required")
	}
	if reason == "" {
		return nil, apperrors.InvalidInput("reason required - document why the chain broke")
	}

	broken, err := s.repo.Get(ctx, tenantID, firstBrokenEventID)
	if err != nil {
		return nil, err
	}

	// Compute the correct prev_hash for the broken event by fetching
	// the event immediately before it in the chain.
	prev, err := s.repo.Previous(ctx, tenantID, firstBrokenEventID)
	if err != nil {
		return nil, err
	}
	startPrevHash := GenesisHash
	if prev != nil {
		startPrevHash = prev.Hash
	}

	updated, err := s.repo.ReanchorFrom(ctx, tenantID, broken.SequenceNumber, startPrevHash)
	if err != nil {
		return nil, err
	}

	// Append an immutable meta-event recording the re-anchor.
	meta := &AuditEvent{
		Actor:  Actor{ID: "system", Type: ActorSystem},
		Action: "chain.reanchored",
		Resource: Resource{
			ID:   firstBrokenEventID,
			Type: "audit_chain",
		},
		Outcome: OutcomeSuccess,
		Metadata: map[string]interface{}{
			"reason":               reason,
			"first_broken_seq":     broken.SequenceNumber,
			"events_reanchored":    updated,
			"reanchor_system_note": "Hashes recomputed forward from break point. See reason for root cause.",
		},
	}
	s.applyDefaults(meta)
	out, err := s.repo.AppendChained(ctx, tenantID, meta)
	if err != nil {
		return nil, err
	}
	s.logger.Warn().
		Str("tenant_id", tenantID).
		Str("first_broken_event_id", firstBrokenEventID).
		Int64("events_reanchored", updated).
		Str("reason", reason).
		Msg("chain reanchored")
	return out, nil
}

// ─── Subscriptions (future SSE) ───────────────────────────

// Subscribe returns a non-blocking channel that receives every successfully
// written event. Slow consumers drop messages - this is for live UX, not
// durable delivery.
func (s *Service) Subscribe() (<-chan *AuditEvent, func()) {
	ch := make(chan *AuditEvent, 64)
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()

	cancel := func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		for i, c := range s.subs {
			if c == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

func (s *Service) fanout(e *AuditEvent) {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default: // drop on slow consumer
		}
	}
}
