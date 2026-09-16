package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/notify"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// AuditHandler exposes the compliance-grade audit log REST surface.
type AuditHandler struct {
	ossMode           bool
	svc               *audit.Service
	tenants           audit.TenantStore
	alerter           *notify.Alerter
	limiter           *audit.MonthlyLimiter
	logger            zerolog.Logger
	maxBatchBodyBytes int64
	asyncDefault      bool
}

// AuditHandlerConfig configures AuditHandler.
type AuditHandlerConfig struct {
	OSSMode           bool
	MaxBatchBodyBytes int64
	AsyncDefault      bool
	MonthlyLimiter    *audit.MonthlyLimiter
}

// NewAuditHandler constructs the handler. `tenants` is optional - if nil
// the PDF export will use the tenant id as the display name.
func NewAuditHandler(svc *audit.Service, tenants audit.TenantStore, alerter *notify.Alerter, logger zerolog.Logger, cfg AuditHandlerConfig) *AuditHandler {
	maxBody := cfg.MaxBatchBodyBytes
	if maxBody <= 0 {
		maxBody = 4 << 20
	}
	return &AuditHandler{
		ossMode:           cfg.OSSMode,
		svc:               svc,
		tenants:           tenants,
		alerter:           alerter,
		limiter:           cfg.MonthlyLimiter,
		logger:            logger.With().Str("component", "audit_http").Logger(),
		maxBatchBodyBytes: maxBody,
		asyncDefault:      cfg.AsyncDefault,
	}
}

// Mount is a compatibility convenience that wires every audit route
// (reads, writes AND the SSE stream) under a single middleware chain.
// Tests use this; production wiring should prefer the granular
// MountWrite / MountRead / MountStream pair so each surface can run
// under its own auth (DualAuth for reads, SSEAuth for the stream,
// raw APIKey for writes).
func (h *AuditHandler) Mount(r chi.Router) {
	h.MountWrite(r)
	h.MountRead(r)
	h.MountStream(r)
}

// MountWrite installs ingestion routes. Mount under auth.APIKey (write
// scope is enforced internally by RequireWrite); never reachable from
// the dashboard.
func (h *AuditHandler) MountWrite(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireWrite)
		r.Post("/v1/events", h.logEvent)
		r.Post("/v1/events/batch", h.logBatch)
	})
}

// MountRead installs read-only audit routes. Mount under auth.DualAuth
// so SDK clients (API key) and the dashboard (Better Auth JWT) can both
// reach them. Read scope is sufficient.
func (h *AuditHandler) MountRead(r chi.Router) {
	r.Get("/v1/events", h.listEvents)
	if !h.ossMode {
		r.Get("/v1/receipts/{id}", h.getReceipt)
	}
	r.Get("/v1/events/{id}", h.getEvent)
	r.Get("/v1/events/{id}/verify", h.verifyOne)
	r.Get("/v1/verify", h.verify)
	r.Get("/v1/export/pdf", h.exportPDF)
	r.Get("/v1/export/evidence-pack", h.exportEvidencePack)
	if !h.ossMode {
		r.With(auth.RequireWriteOrAdmin).Post("/v1/chain/reanchor", h.reanchorChain)
		r.With(auth.RequireWriteOrAdmin).Post("/v1/chain/reanchor-all", h.reanchorAll)
	}
}

// MountStream installs the SSE stream route. Mount under auth.SSEAuth
// so the dashboard uses single-use tickets (no credentials in URLs)
// and SDK clients can still use a bearer API key header.
func (h *AuditHandler) MountStream(r chi.Router) {
	r.Get("/v1/events/stream", h.streamEvents)
}

// MountShare installs read-only audit routes for share-link access.
func (h *AuditHandler) MountShare(r chi.Router) {
	r.Get("/v1/share/events", h.listEvents)
	r.Get("/v1/share/events/{id}", h.getEvent)
	r.Get("/v1/share/verify", h.verify)
	r.Get("/v1/share/export/pdf", h.exportPDF)
	r.Get("/v1/share/export/evidence-pack", h.exportEvidencePack)
}

// streamEvents writes every new audit event for the resolved tenant to
// the client as an SSE stream. Slow consumers drop messages - this is
// intended for live UI feedback, not durable delivery.
//
// Performance and Scalability Notes:
//  1. Threading and Memory Model: The current implementation performs per-tenant filtering
//     on a global stream inside the app process. Each active connection creates a memory channel
//     subscription in the internal audit service (h.svc.Subscribe()). This holds event slices in memory,
//     with a small footprint per connection, but filters events CPU-side in-process.
//  2. Scalability Limits: This design works well for small to medium deployments. Once the number of
//     concurrent dashboard clients grows large (e.g., >100 active connections), or when scaling the
//     backend API to multiple stateless instances, this in-memory fan-out will become a bottleneck.
//     At that point, we should transition to a centralized pub/sub system like Redis Pub/Sub or
//     Postgres LISTEN/NOTIFY, enabling horizontal scaling of the API servers.
func (h *AuditHandler) streamEvents(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)

	ch, cancel := h.svc.Subscribe()
	defer cancel()

	// Hello frame so the EventSource opens immediately.
	_, _ = w.Write([]byte("event: open\ndata: {}\n\n"))
	flusher.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	enc := json.NewEncoder(w)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case e, open := <-ch:
			if !open {
				return
			}
			if e == nil || e.TenantID != tenantID {
				continue
			}
			if _, err := w.Write([]byte("event: event\ndata: ")); err != nil {
				return
			}
			if err := enc.Encode(e); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ─── Wire types ────────────────────────────────────────────

type wireEvent struct {
	ID        string                 `json:"id,omitempty"`
	Timestamp *time.Time             `json:"timestamp,omitempty"`
	Actor     audit.Actor            `json:"actor"`
	Action    string                 `json:"action"`
	Resource  audit.Resource         `json:"resource"`
	Outcome   audit.Outcome          `json:"outcome"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func (w wireEvent) toDomain() *audit.AuditEvent {
	e := &audit.AuditEvent{
		ID:       w.ID,
		Actor:    w.Actor,
		Action:   w.Action,
		Resource: w.Resource,
		Outcome:  w.Outcome,
		Metadata: w.Metadata,
	}
	if w.Timestamp != nil {
		e.Timestamp = *w.Timestamp
	}
	return e
}

type batchRequest struct {
	Events []wireEvent `json:"events"`
}

// ─── Handlers ──────────────────────────────────────────────

func (h *AuditHandler) logEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBatchBodyBytes)
	var in wireEvent
	if err := requestJSONDecoder(r).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tenantID := auth.TenantFromContext(r.Context())
	event := in.toDomain()

	if err := h.checkMonthlyLimit(r.Context(), tenantID, 1); err != nil {
		writeAppError(w, err)
		return
	}

	if h.useAsync(r) {
		h.enqueueBatch(w, r, tenantID, []*audit.AuditEvent{event})
		return
	}

	out, err := h.svc.Log(r.Context(), tenantID, event)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *AuditHandler) logBatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBatchBodyBytes)
	var in batchRequest
	if err := requestJSONDecoder(r).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tenantID := auth.TenantFromContext(r.Context())

	events := make([]*audit.AuditEvent, len(in.Events))
	for i, e := range in.Events {
		events[i] = e.toDomain()
	}

	if err := h.checkMonthlyLimit(r.Context(), tenantID, len(events)); err != nil {
		writeAppError(w, err)
		return
	}

	if h.useAsync(r) {
		h.enqueueBatch(w, r, tenantID, events)
		return
	}

	res, err := h.svc.LogBatch(r.Context(), tenantID, events)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("log batch failed")
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// enqueueBatch tries async outbox ingest first; on infrastructure failure
// (e.g. missing ingest_receipts table before migrate runs) falls back to
// synchronous hash-chain commit so clients never see a bare 500.
func (h *AuditHandler) enqueueBatch(w http.ResponseWriter, r *http.Request, tenantID string, events []*audit.AuditEvent) {
	receipt, err := h.svc.EnqueueBatch(r.Context(), tenantID, events)
	if err == nil {
		writeJSON(w, http.StatusAccepted, receipt)
		return
	}

	h.logger.Warn().Err(err).Str("tenant_id", tenantID).Int("events", len(events)).
		Msg("async audit ingest failed, falling back to sync commit")

	res, syncErr := h.svc.LogBatch(r.Context(), tenantID, events)
	if syncErr != nil {
		h.logger.Error().Err(syncErr).Str("tenant_id", tenantID).Msg("sync audit ingest fallback failed")
		writeAppError(w, syncErr)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AuditHandler) getReceipt(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	receipt, err := h.svc.GetReceipt(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (h *AuditHandler) useAsync(r *http.Request) bool {
	if h.ossMode {
		return false
	}
	if isSyncRequest(r) {
		return false
	}
	if h.asyncDefault {
		return true
	}
	return h.svc.AsyncDefault()
}

func isSyncRequest(r *http.Request) bool {
	return r.Header.Get("X-Fact0-Sync") == "true" ||
		r.URL.Query().Get("sync") == "true"
}

func (h *AuditHandler) checkMonthlyLimit(ctx context.Context, tenantID string, incoming int) error {
	if h.ossMode || h.limiter == nil {
		return nil
	}
	return h.limiter.Check(ctx, tenantID, incoming)
}

func (h *AuditHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	q := r.URL.Query()

	f := audit.Filter{
		SessionID:  q.Get("session_id"),
		ActorID:    q.Get("actor_id"),
		ActorType:  audit.ActorType(q.Get("actor_type")),
		Action:     q.Get("action"),
		ResourceID: q.Get("resource_id"),
		Outcome:    audit.Outcome(q.Get("outcome")),
		Page:       atoiOr(q.Get("page"), 1),
		PageSize:   atoiOr(q.Get("page_size"), 50),
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be RFC3339")
			return
		}
		f.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
		f.To = t
	}
	f.From, f.To = applyShareListFilter(r.Context(), f.From, f.To)

	events, total, err := h.svc.List(r.Context(), tenantID, f)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events":    events,
		"total":     total,
		"page":      f.Page,
		"page_size": f.PageSize,
	})
}

func (h *AuditHandler) getEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	id := chi.URLParam(r, "id")
	e, err := h.svc.Get(r.Context(), tenantID, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if sc, ok := auth.ShareFromContext(r.Context()); ok {
		if sc.From != nil && e.Timestamp.Before(*sc.From) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if sc.To != nil && e.Timestamp.After(*sc.To) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *AuditHandler) verifyOne(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	id := chi.URLParam(r, "id")
	res, err := h.svc.VerifyOne(r.Context(), tenantID, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AuditHandler) verify(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	from, to, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// scan_all=true returns every broken event instead of stopping at the first.
	if r.URL.Query().Get("scan_all") == "true" {
		res, verifyErr := h.svc.VerifyDeep(r.Context(), tenantID, from, to)
		if verifyErr != nil {
			writeAppError(w, verifyErr)
			return
		}
		h.maybeAlertVerify(r.Context(), tenantID, res)
		writeJSON(w, http.StatusOK, res)
		return
	}
	res, err := h.svc.Verify(r.Context(), tenantID, from, to)
	if err != nil {
		writeAppError(w, err)
		return
	}
	h.maybeAlertVerify(r.Context(), tenantID, res)
	writeJSON(w, http.StatusOK, res)
}

func (h *AuditHandler) maybeAlertVerify(ctx context.Context, tenantID string, res *audit.VerifyResult) {
	if h.alerter == nil || res == nil || res.Valid {
		return
	}
	broken := res.BrokenCount
	if broken == 0 && res.FirstBrokenEventID != "" {
		broken = 1
	}
	reason := res.Reason
	if reason == "" && res.FirstBrokenEventID != "" {
		reason = "first break at " + res.FirstBrokenEventID
	}
	workspace := tenantID
	if h.tenants != nil {
		if t, err := h.tenants.GetTenant(ctx, tenantID); err == nil && t.Name != "" {
			workspace = t.Name
		}
	}
	h.alerter.ChainBreak(ctx, tenantID, workspace, res.Valid, broken, reason)
}

func (h *AuditHandler) exportPDF(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	if err := h.checkExportLimit(r.Context(), tenantID); err != nil {
		writeAppError(w, err)
		return
	}
	from, to, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tenantName := tenantID
	if h.tenants != nil {
		if t, err := h.tenants.GetTenant(r.Context(), tenantID); err == nil {
			tenantName = t.Name
		}
	}

	verifyURL := "http://" + r.Host + "/v1/verify"

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		`attachment; filename="audit-`+tenantID+`.pdf"`)

	if err := audit.RenderPDF(r.Context(), w, h.svc, audit.PDFInput{
		TenantID:   tenantID,
		TenantName: tenantName,
		From:       from,
		To:         to,
		VerifyURL:  verifyURL,
	}); err != nil {
		h.logger.Error().Err(err).Msg("pdf render failed")
		h.logExportAudit(r.Context(), tenantID, from, to, audit.OutcomeError)
		return
	}
	h.logExportAudit(r.Context(), tenantID, from, to, audit.OutcomeSuccess)
}

func (h *AuditHandler) exportEvidencePack(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantFromContext(r.Context())
	if err := h.checkExportLimit(r.Context(), tenantID); err != nil {
		writeAppError(w, err)
		return
	}
	from, to, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to = auth.MergeShareRange(r.Context(), from, to)

	tenantName := tenantID
	if h.tenants != nil {
		if t, err := h.tenants.GetTenant(r.Context(), tenantID); err == nil {
			tenantName = t.Name
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		`attachment; filename="evidence-`+tenantID+`.zip"`)

	if err := audit.EvidencePack(r.Context(), h.svc, w, audit.PDFInput{
		TenantID:   tenantID,
		TenantName: tenantName,
		From:       from,
		To:         to,
		VerifyURL:  "https://" + r.Host + "/verify",
	}); err != nil {
		h.logger.Error().Err(err).Msg("evidence pack failed")
		writeAppError(w, err)
		return
	}
	h.logExportAudit(r.Context(), tenantID, from, to, audit.OutcomeSuccess)

	if r.URL.Query().Get("notify") == "1" && h.alerter != nil {
		eventCount := int64(0)
		if _, total, err := h.svc.List(r.Context(), tenantID, audit.Filter{From: from, To: to, PageSize: 1}); err == nil {
			eventCount = int64(total)
		}
		downloadURL := "https://app.fact0.io/dashboard/audit"
		if appURL := r.Header.Get("X-Fact0-App-Origin"); appURL != "" {
			downloadURL = appURL + "/dashboard/audit"
		}
		dateRange := from.Format("Jan 2, 2006") + " - " + to.Format("Jan 2, 2006")
		h.alerter.ReportReady(
			r.Context(),
			tenantID,
			tenantName,
			"SOC 2 Trust Services Evidence Pack",
			downloadURL,
			dateRange,
			int(eventCount),
		)
	}
}

type reanchorRequest struct {
	FirstBrokenEventID string `json:"first_broken_event_id"`
	Reason             string `json:"reason"`
}

func (h *AuditHandler) reanchorChain(w http.ResponseWriter, r *http.Request) {
	var in reanchorRequest
	if err := requestJSONDecoder(r).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.FirstBrokenEventID == "" {
		writeError(w, http.StatusBadRequest, "first_broken_event_id required")
		return
	}
	if in.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason required")
		return
	}
	tenantID := auth.TenantFromContext(r.Context())
	meta, err := h.svc.ReanchorChain(r.Context(), tenantID, in.FirstBrokenEventID, in.Reason)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "reanchored",
		"meta_event_id":  meta.ID,
		"meta_event_seq": meta.SequenceNumber,
	})
}

type reanchorAllRequest struct {
	Reason string `json:"reason"`
}

func (h *AuditHandler) reanchorAll(w http.ResponseWriter, r *http.Request) {
	var in reanchorAllRequest
	if err := requestJSONDecoder(r).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason required")
		return
	}
	tenantID := auth.TenantFromContext(r.Context())
	meta, err := h.svc.ReanchorAll(r.Context(), tenantID, in.Reason)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "reanchored",
		"meta_event_id":  meta.ID,
		"meta_event_seq": meta.SequenceNumber,
	})
}

// ─── Helpers ───────────────────────────────────────────────

func parseRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	var from, to time.Time
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return from, to, errBadParam("from must be RFC3339")
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return from, to, errBadParam("to must be RFC3339")
		}
		to = t
	}
	from, to = auth.MergeShareRange(r.Context(), from, to)
	return from, to, nil
}

func applyShareListFilter(ctx context.Context, from, to time.Time) (time.Time, time.Time) {
	sc, ok := auth.ShareFromContext(ctx)
	if !ok {
		return from, to
	}
	if sc.From != nil && (from.IsZero() || sc.From.After(from)) {
		from = *sc.From
	}
	if sc.To != nil && (to.IsZero() || sc.To.Before(to)) {
		to = *sc.To
	}
	return from, to
}

type httpErr struct{ msg string }

func (e *httpErr) Error() string { return e.msg }

func errBadParam(msg string) error { return &httpErr{msg: msg} }

// logExportAudit records audit.export when the dashboard (or SDK) downloads
// a PDF compliance pack. Without this row, "who exported logs?" queries
// return empty even though the user clearly clicked Export PDF.
func (h *AuditHandler) logExportAudit(
	ctx context.Context,
	tenantID string,
	from, to time.Time,
	outcome audit.Outcome,
) {
	actor := audit.Actor{
		ID:   "system",
		Type: audit.ActorSystem,
	}
	if p, ok := auth.PrincipalFromContext(ctx); ok && p != nil {
		actor.ID = p.UserID
		actor.Type = audit.ActorHuman
	}

	_, _ = h.svc.Log(ctx, tenantID, &audit.AuditEvent{
		Actor:   actor,
		Action:  "audit.export",
		Outcome: outcome,
		Resource: audit.Resource{
			ID:   "evidence_pack",
			Type: "compliance_pack",
			Name: "Compliance Export",
		},
		Metadata: map[string]interface{}{
			"export_from": from.Format(time.RFC3339),
			"export_to":   to.Format(time.RFC3339),
		},
	})
}

func atoiOr(s string, d int) int {
	if s == "" {
		return d
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return d
}

type planResolver interface {
	GetTenantPlan(ctx context.Context, tenantID string) (*audit.Plan, error)
}

func (h *AuditHandler) checkExportLimit(ctx context.Context, tenantID string) error {
	if h.ossMode {
		return nil
	}
	resolver, ok := h.tenants.(planResolver)
	if !ok {
		return nil // skip check if tenant resolver is mock/missing in tests
	}
	plan, err := resolver.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return nil // skip if unknown tenant
	}

	var maxExports int
	switch plan.ID {
	case "free":
		maxExports = 5
	case "pro":
		maxExports = 50
	case "team":
		maxExports = 200
	default:
		return nil // Enterprise/custom have unlimited exports
	}

	// Count successful exports in the last 30 days
	_, total, err := h.svc.List(ctx, tenantID, audit.Filter{
		Action:   "audit.export",
		Outcome:  audit.OutcomeSuccess,
		From:     time.Now().UTC().Add(-30 * 24 * time.Hour),
		PageSize: 1, // We only need the total count
	})
	if err != nil {
		return nil // fallback safely on database lookup errors
	}

	if total >= maxExports {
		planName := plan.ID
		if len(planName) > 0 {
			planName = strings.ToUpper(planName[:1]) + planName[1:]
		}
		return apperrors.InvalidInput(planName + " plan is limited to " + strconv.Itoa(maxExports) + " evidence exports per month. Please upgrade your workspace to a higher plan to get more compliance pack downloads.")
	}
	return nil
}
