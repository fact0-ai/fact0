package postgres_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/ingestion"
	"github.com/fact0-ai/fact0/internal/replay"
	"github.com/rs/zerolog"
)

func TestCoreAuditSupportedHistoryWindow(t *testing.T) {
	store, closeDB := testAuditStore(t)
	defer closeDB()
	ctx := context.Background()
	tenant := "core_window_" + audit.NewEventID()
	ensureTenant(t, store, tenant)
	svc := audit.NewService(store, zerolog.Nop(), false)
	epoch := time.Unix(0, 0).UTC()
	last := time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)
	var events []*audit.AuditEvent
	for _, timestamp := range []time.Time{epoch.Add(-time.Microsecond), epoch, time.Now().UTC().Add(24 * time.Hour), last} {
		event := sampleEvent(audit.NewEventID(), "window.capture")
		event.Timestamp = timestamp
		events = append(events, event)
	}
	if _, err := svc.Log(ctx, tenant, events[0]); err == nil {
		t.Fatal("single ingest accepted pre-epoch timestamp")
	}
	batch, err := svc.LogBatch(ctx, tenant, events)
	if err != nil || batch.Accepted != 3 || batch.Rejected != 1 {
		t.Fatalf("supported timestamp range: %+v %v", batch, err)
	}
	for _, verify := range []func(context.Context, string, time.Time, time.Time) (*audit.VerifyResult, error){svc.Verify, svc.VerifyDeep} {
		result, err := verify(ctx, tenant, time.Time{}, time.Time{})
		if err != nil || !result.Valid || result.EventsChecked != 3 {
			t.Fatalf("default verification omitted accepted history: %+v %v", result, err)
		}
	}
	var buf bytes.Buffer
	if err := audit.EvidencePack(ctx, svc, &buf, audit.PDFInput{TenantID: tenant}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	file, err := zr.Open("verification.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var verification audit.VerifyResult
	if err := json.NewDecoder(file).Decode(&verification); err != nil {
		t.Fatal(err)
	}
	if !verification.Valid || verification.EventsChecked != 3 || !verification.From.Equal(epoch) || verification.To.Before(last) {
		t.Fatalf("default export omitted accepted history: %+v", verification)
	}
	bounded, err := svc.Verify(ctx, tenant, time.Time{}, time.Now().UTC())
	if err != nil || !bounded.Valid || bounded.EventsChecked != 1 {
		t.Fatalf("explicit upper bound not honored: %+v %v", bounded, err)
	}
}

func TestCoreAuditCanonicalRoundTripAndDateWindow(t *testing.T) {
	store, closeDB := testAuditStore(t)
	defer closeDB()
	ctx := context.Background()
	tenant := "core_audit_" + audit.NewEventID()
	ensureTenant(t, store, tenant)
	svc := audit.NewService(store, zerolog.Nop(), false)
	base := time.Date(2026, 9, 16, 12, 0, 0, 123456789, time.UTC)
	events := []*audit.AuditEvent{sampleEvent(audit.NewEventID(), "claude_code.session.started"), sampleEvent(audit.NewEventID(), "other"), sampleEvent(audit.NewEventID(), "claude_code.tool.used")}
	events[0].Timestamp = base
	events[1].Timestamp = base.Add(-24 * time.Hour)
	events[2].Timestamp = base.Add(time.Second)
	events[0].Resource = audit.Resource{ID: "session-long", Type: "claude_code.session"}
	events[0].Metadata = map[string]any{"integer": json.Number("9007199254740993"), "decimal": json.Number("1.0000"), "exponent": json.Number("1e3"), "text": "first\nline🙂", "nested": map[string]any{"v": json.Number("0.00000100")}}
	events[2].Metadata = map[string]any{"session_id": "session-long"}
	if _, err := svc.LogBatch(ctx, tenant, events); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		stored, err := svc.Get(ctx, tenant, event.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Hash != audit.ComputeHash(stored) {
			t.Fatalf("roundtrip digest mismatch: %s", event.ID)
		}
		if stored.Timestamp.Nanosecond()%1000 != 0 {
			t.Fatal("sub-microsecond timestamp persisted")
		}
	}
	for _, deep := range []bool{false, true} {
		var result *audit.VerifyResult
		var err error
		if deep {
			result, err = svc.VerifyDeep(ctx, tenant, base.Add(-time.Second), base.Add(2*time.Second))
		} else {
			result, err = svc.Verify(ctx, tenant, base.Add(-time.Second), base.Add(2*time.Second))
		}
		if err != nil || !result.Valid || result.EventsChecked != 2 {
			t.Fatalf("window verification (deep=%v): %+v %v", deep, result, err)
		}
	}
	rows, total, err := svc.List(ctx, tenant, audit.Filter{Action: "claude_code.*", SessionID: "session-long", Page: 1, PageSize: 1})
	if err != nil || total != 2 || len(rows) != 1 {
		t.Fatalf("session pagination: %d %d %v", len(rows), total, err)
	}
	rows, _, err = svc.List(ctx, tenant, audit.Filter{SessionID: "session-long", Page: 2, PageSize: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("second page: %v %v", rows, err)
	}
	// A retry, including duplicate IDs within the same submitted batch, never grows the chain.
	if _, err = svc.LogBatch(ctx, tenant, []*audit.AuditEvent{events[0], events[0]}); err != nil {
		t.Fatal(err)
	}
	_, total, err = svc.List(ctx, tenant, audit.Filter{})
	if err != nil || total != 3 {
		t.Fatalf("retry created events: %d %v", total, err)
	}
	if _, err = store.WritePool().Exec(ctx, `UPDATE audit_events SET actor_type='human' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("audit mutation allowed")
	}
	if _, err = store.WritePool().Exec(ctx, `DELETE FROM audit_events WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("audit deletion allowed")
	}
	tx, err := store.WritePool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL fact0.reanchor_mode='on'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE audit_events SET hash='rewritten' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("legacy GUC bypassed immutability")
	}
}

func TestCoreTelemetryLifecycleRetriesAndIsolation(t *testing.T) {
	store, as, closeDB := testStore(t)
	defer closeDB()
	ctx := context.Background()
	tenant := "core_tel_" + audit.NewEventID()
	other := tenant + "_other"
	ensureTenant(t, as, tenant)
	ensureTenant(t, as, other)
	svc := ingestion.NewService(store, zerolog.Nop(), false)
	req := ingestion.StartExecutionRequest{AgentID: "collector", IdempotencyKey: "session-1"}
	exec, err := svc.StartExecution(ctx, tenant, req)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry, e := svc.StartExecution(ctx, tenant, req)
			if e != nil || retry.ID != exec.ID {
				t.Errorf("execution retry: %+v %v", retry, e)
			}
		}()
	}
	wg.Wait()
	foreign, err := svc.StartExecution(ctx, other, req)
	if err != nil {
		t.Fatal(err)
	}
	if foreign.ID == exec.ID {
		t.Fatal("cross-tenant execution ID collision")
	}
	start := exec.StartedAt.UTC().Truncate(time.Microsecond)
	end := start.Add(100 * time.Millisecond)
	parent := &execution.Span{ID: exec.ID + "_turn", Name: "turn", SpanType: execution.SpanKindCustom, Status: execution.StatusRunning, StartedAt: start, Metadata: map[string]string{"prompt": "full prompt"}}
	ingest := func(span *execution.Span) {
		t.Helper()
		n, errs := svc.IngestSpans(ctx, tenant, exec.ID, []*execution.Span{span})
		if n != 1 || len(errs) > 0 {
			t.Fatalf("span ingest: %d %+v", n, errs)
		}
	}
	ingest(parent)
	initial := *parent
	childEnd := start.Add(20 * time.Millisecond)
	child := &execution.Span{ID: exec.ID + "_tool", Name: "Bash", ParentSpanID: &parent.ID, SpanType: execution.SpanKindToolCall, Status: execution.StatusCompleted, StartedAt: start.Add(10 * time.Millisecond), EndedAt: &childEnd, Metadata: map[string]string{"output": "full\noutput🙂"}}
	ingest(child)
	parent.Status = execution.StatusCompleted
	parent.EndedAt = &end
	parent.Metadata = map[string]string{"prompt": "full prompt", "response": "complete response"}
	ingest(parent)
	ingest(parent)
	ingest(&initial)
	ingest(child)
	stored, err := store.GetSpan(ctx, parent.ID)
	if err != nil || stored.Status != execution.StatusCompleted || stored.Metadata["response"] != "complete response" {
		t.Fatalf("completion projection: %+v %v", stored, err)
	}
	history, err := store.GetEventsBySpan(ctx, parent.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("parent lifecycle history: %d %v", len(history), err)
	}
	if history[0].Payload == nil || history[1].Payload == nil {
		t.Fatal("lifecycle snapshots missing")
	}
	if _, err = as.WritePool().Exec(ctx, `DELETE FROM execution_events WHERE span_id=$1`, parent.ID); err == nil {
		t.Fatal("lifecycle deletion allowed")
	}
	bad := *parent
	bad.Metadata = map[string]string{"response": "replacement"}
	if n, _ := svc.IngestSpans(ctx, tenant, exec.ID, []*execution.Span{&bad}); n != 0 {
		t.Fatal("completed span rewritten")
	}
	if n, _ := svc.IngestSpans(ctx, other, foreign.ID, []*execution.Span{&bad}); n != 0 {
		t.Fatal("foreign span ID accepted")
	}
	foreignParent := &execution.Span{ID: foreign.ID + "_parent", Name: "foreign", SpanType: execution.SpanKindCustom, Status: execution.StatusRunning, StartedAt: start}
	if n, errs := svc.IngestSpans(ctx, other, foreign.ID, []*execution.Span{foreignParent}); n != 1 {
		t.Fatal(errs)
	}
	badParent := *child
	badParent.ID = exec.ID + "_badparent"
	badParent.ParentSpanID = &foreignParent.ID
	if n, _ := svc.IngestSpans(ctx, tenant, exec.ID, []*execution.Span{&badParent}); n != 0 {
		t.Fatal("cross-execution parent accepted")
	}
	badParent.ParentSpanID = nil
	badParent.CausedBySpanIDs = []string{foreignParent.ID}
	if n, _ := svc.IngestSpans(ctx, tenant, exec.ID, []*execution.Span{&badParent}); n != 0 {
		t.Fatal("cross-execution causality accepted")
	}
	injection := &execution.ExecutionEvent{SpanID: parent.ID, EventType: "ATTACK", Timestamp: start}
	if n, _ := svc.IngestEvents(ctx, other, foreign.ID, []*execution.ExecutionEvent{injection}); n != 0 {
		t.Fatal("cross-tenant event injection accepted")
	}
	if _, err = as.WritePool().Exec(ctx, `INSERT INTO execution_events(id,execution_id,span_id,event_type,timestamp) VALUES($1,$2,$3,'TEST',$4)`, foreign.ID+"_injected", foreign.ID, parent.ID, start); err == nil {
		t.Fatal("database accepted cross-execution event")
	}
	replayed, err := replay.NewService(store, zerolog.Nop()).ReplayExecution(ctx, tenant, replay.ReplayRequest{ExecutionID: exec.ID})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.DurationMs != 100 || len(replayed.Frames) != 4 {
		t.Fatalf("replay summary %+v", replayed)
	}
	for i, frame := range replayed.Frames {
		if frame.DeltaMs < 0 || (i > 0 && frame.ElapsedMs < replayed.Frames[i-1].ElapsedMs) {
			t.Fatal("replay time regressed")
		}
	}
	// Implicit IDs do not depend on batch position.
	for i := 0; i < 2; i++ {
		sp := &execution.Span{Name: "same", SpanType: execution.SpanKindCustom, Status: execution.StatusRunning, StartedAt: start.Add(time.Duration(i+1) * time.Second)}
		ingest(sp)
	}
	spans, err := store.GetSpansByExecution(ctx, exec.ID)
	if err != nil || len(spans) != 4 {
		t.Fatalf("implicit IDs collided: %d %v", len(spans), err)
	}
	evt := &execution.ExecutionEvent{SpanID: child.ID, EventType: "NOTE", Timestamp: start.Add(time.Second), Payload: &execution.PayloadRef{Inline: map[string]any{"n": json.Number("9007199254740993")}}}
	for i := 0; i < 2; i++ {
		cp := *evt
		cp.ID = ""
		n, errs := svc.IngestEvents(ctx, tenant, exec.ID, []*execution.ExecutionEvent{&cp})
		if n != 1 {
			t.Fatalf("event retry %d: %+v", i, errs)
		}
	}
	evts, err := store.GetEventsBySpan(ctx, child.ID)
	if err != nil || len(evts) != 3 {
		t.Fatalf("duplicate event retry: %d %v", len(evts), err)
	}
	first, err := svc.EndExecution(ctx, tenant, exec.ID, execution.StatusCompleted)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.EndExecution(ctx, tenant, exec.ID, execution.StatusCompleted)
	if err != nil || !again.EndedAt.Equal(*first.EndedAt) {
		t.Fatalf("end retry changed timestamp: %v", err)
	}
}

func TestCoreOfflineExecutionCaptureTimes(t *testing.T) {
	store, as, closeDB := testStore(t)
	defer closeDB()
	ctx := context.Background()
	tenant := "core_offline_" + audit.NewEventID()
	ensureTenant(t, as, tenant)
	svc := ingestion.NewService(store, zerolog.Nop(), false)
	// Capture happened well before delivery, on a client with an explicit zone.
	started := time.Date(2026, 9, 1, 13, 21, 27, 123456789, time.FixedZone("capture", 5*3600+30*60))
	ended := started.Add(24 * time.Second)
	wantStart := started.UTC().Truncate(time.Microsecond)
	wantEnd := ended.UTC().Truncate(time.Microsecond)
	req := ingestion.StartExecutionRequest{AgentID: "collector", IdempotencyKey: "offline", StartedAt: &started}
	exec, err := svc.StartExecution(ctx, tenant, req)
	if err != nil || !exec.StartedAt.Equal(wantStart) {
		t.Fatalf("captured start: %+v %v", exec, err)
	}
	for i := 0; i < 2; i++ {
		retry, err := svc.StartExecution(ctx, tenant, req)
		if err != nil || !retry.StartedAt.Equal(wantStart) || retry.ID != exec.ID {
			t.Fatalf("start retry: %+v %v", retry, err)
		}
	}
	changedStart := started.Add(time.Second)
	changedReq := req
	changedReq.StartedAt = &changedStart
	if _, err := svc.StartExecution(ctx, tenant, changedReq); err == nil {
		t.Fatal("changed captured start was accepted")
	}
	beforeStart := started.Add(-time.Second)
	if _, err := svc.EndExecutionAt(ctx, tenant, exec.ID, execution.StatusCompleted, &beforeStart); err == nil {
		t.Fatal("end before start was accepted")
	}
	spanStart := started.Add(2 * time.Second)
	spanEnd := started.Add(22 * time.Second)
	span := &execution.Span{ID: exec.ID + "_turn", Name: "offline turn", SpanType: execution.SpanKindCustom, Status: execution.StatusCompleted, StartedAt: spanStart, EndedAt: &spanEnd}
	if n, errs := svc.IngestSpans(ctx, tenant, exec.ID, []*execution.Span{span}); n != 1 || len(errs) != 0 {
		t.Fatalf("captured span: %d %+v", n, errs)
	}
	for i := 0; i < 2; i++ {
		got, err := svc.EndExecutionAt(ctx, tenant, exec.ID, execution.StatusCompleted, &ended)
		if err != nil || got.EndedAt == nil || !got.EndedAt.Equal(wantEnd) {
			t.Fatalf("captured end retry: %+v %v", got, err)
		}
	}
	changedEnd := ended.Add(time.Second)
	if _, err := svc.EndExecutionAt(ctx, tenant, exec.ID, execution.StatusCompleted, &changedEnd); err == nil {
		t.Fatal("changed captured end was accepted")
	}
	// Legacy retries still return the original end, rather than delivery time.
	if got, err := svc.EndExecution(ctx, tenant, exec.ID, execution.StatusCompleted); err != nil || !got.EndedAt.Equal(wantEnd) {
		t.Fatalf("legacy end retry changed capture time: %+v %v", got, err)
	}
	got, err := replay.NewService(store, zerolog.Nop()).ReplayExecution(ctx, tenant, replay.ReplayRequest{ExecutionID: exec.ID})
	if err != nil || got.DurationMs != 24_000 || len(got.Frames) != 2 || got.Frames[0].ElapsedMs != 2_000 || got.Frames[1].ElapsedMs != 22_000 {
		t.Fatalf("offline replay duration or offsets: %+v %v", got, err)
	}
	zero := time.Time{}
	if _, err := svc.StartExecution(ctx, tenant, ingestion.StartExecutionRequest{AgentID: "collector", StartedAt: &zero}); err == nil {
		t.Fatal("zero captured start accepted")
	}
	if _, err := svc.EndExecutionAt(ctx, tenant, exec.ID, execution.StatusCompleted, &zero); err == nil {
		t.Fatal("zero captured end accepted")
	}
}
