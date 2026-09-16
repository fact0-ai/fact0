package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/fact0-ai/fact0/internal/canonical"
	"github.com/fact0-ai/fact0/internal/execution"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"github.com/jackc/pgx/v5"
)

func equalJSON(a, b any) bool {
	x, err := canonical.JSON(a)
	if err != nil {
		return false
	}
	y, err := canonical.JSON(b)
	return err == nil && bytes.Equal(x, y)
}
func unmarshalJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// InsertSpan records a span and its immutable lifecycle snapshots atomically.
// A RUNNING projection may complete once; finished content cannot be rewritten.
func (s *Store) InsertSpan(ctx context.Context, span *execution.Span) error {
	span.StartedAt = span.StartedAt.UTC().Truncate(time.Microsecond)
	if span.EndedAt != nil {
		t := span.EndedAt.UTC().Truncate(time.Microsecond)
		span.EndedAt = &t
	}
	if span.Metadata == nil {
		span.Metadata = map[string]string{}
	}
	sort.Strings(span.CausedBySpanIDs)
	span.CausedBySpanIDs = slices.Compact(span.CausedBySpanIDs)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, GetAdvisoryLockKey("span:"+span.ID)); err != nil {
		return err
	}
	old, err := s.scanSpanRow(tx.QueryRow(ctx, `SELECT id,execution_id,parent_span_id,span_type,name,status,started_at,ended_at,metadata,detail,error_info FROM spans WHERE id=$1 FOR UPDATE`, span.ID))
	exists := err == nil
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if exists {
		if old.ExecutionID != span.ExecutionID || old.Name != span.Name || old.SpanType != span.SpanType || !old.StartedAt.Equal(span.StartedAt) || !equalJSON(old.ParentSpanID, span.ParentSpanID) {
			return apperrors.AlreadyExists("span with different identity", span.ID)
		}
		rows, e := tx.Query(ctx, `SELECT caused_by_id FROM span_causality WHERE span_id=$1 ORDER BY caused_by_id`, span.ID)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			old.CausedBySpanIDs = append(old.CausedBySpanIDs, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if equalJSON(old, span) {
			return tx.Commit(ctx)
		}
		// A retried start after completion is a harmless stale projection. Only
		// accept the exact originally recorded start, never a changed payload.
		if old.Status != execution.StatusRunning {
			var snapshot []byte
			e := tx.QueryRow(ctx, `SELECT payload_inline FROM execution_events WHERE id=$1 AND execution_id=$2`, execution.GenerateEventID(span.ExecutionID, span.ID, "SPAN_STARTED", 0), span.ExecutionID).Scan(&snapshot)
			if e == nil && span.Status == execution.StatusRunning && equalJSON(json.RawMessage(snapshot), span) {
				return tx.Commit(ctx)
			}
			return apperrors.AlreadyExists("completed span", span.ID)
		}
		if span.Status == execution.StatusRunning {
			return apperrors.AlreadyExists("running span with different content", span.ID)
		}
		if !equalJSON(old.CausedBySpanIDs, span.CausedBySpanIDs) {
			return apperrors.InvalidInput("span causality cannot change on completion")
		}
	}
	refs := append([]string{}, span.CausedBySpanIDs...)
	if span.ParentSpanID != nil {
		refs = append(refs, *span.ParentSpanID)
	}
	for _, id := range refs {
		var found bool
		if id == span.ID {
			return apperrors.InvalidInput("span cannot reference itself")
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM spans WHERE id=$1 AND execution_id=$2)`, id, span.ExecutionID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return apperrors.InvalidInput("parent and causal spans must exist in the same execution")
		}
	}
	metadata, err := json.Marshal(span.Metadata)
	if err != nil {
		return err
	}
	detail, err := json.Marshal(spanDetail(span))
	if err != nil {
		return err
	}
	var errorInfo any
	if span.Error != nil {
		b, e := json.Marshal(span.Error)
		if e != nil {
			return e
		}
		errorInfo = string(b)
	}
	if !exists {
		_, err = tx.Exec(ctx, `INSERT INTO spans(id,execution_id,parent_span_id,span_type,name,status,started_at,ended_at,metadata,detail,error_info) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, span.ID, span.ExecutionID, span.ParentSpanID, string(span.SpanType), span.Name, string(span.Status), span.StartedAt, span.EndedAt, string(metadata), string(detail), errorInfo)
	} else {
		_, err = tx.Exec(ctx, `UPDATE spans SET status=$2,ended_at=$3,metadata=$4,detail=$5,error_info=$6 WHERE id=$1`, span.ID, string(span.Status), span.EndedAt, string(metadata), string(detail), errorInfo)
	}
	if err != nil {
		return apperrors.Internal("persisting span", err)
	}
	if !exists {
		for _, id := range span.CausedBySpanIDs {
			if _, err = tx.Exec(ctx, `INSERT INTO span_causality(span_id,caused_by_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, span.ID, id); err != nil {
				return err
			}
		}
		start := *span
		start.Status = execution.StatusRunning
		start.EndedAt = nil
		if err = insertLifecycle(ctx, tx, &start, "SPAN_STARTED", span.StartedAt); err != nil {
			return err
		}
	}
	if span.EndedAt != nil {
		kind := "SPAN_ENDED"
		if span.Status == execution.StatusFailed {
			kind = "SPAN_FAILED"
		}
		if span.Status == execution.StatusCancelled {
			kind = "SPAN_CANCELLED"
		}
		if err = insertLifecycle(ctx, tx, span, kind, *span.EndedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertLifecycle(ctx context.Context, tx pgx.Tx, span *execution.Span, kind string, ts time.Time) error {
	return insertEventTx(ctx, tx, &execution.ExecutionEvent{ID: execution.GenerateEventID(span.ExecutionID, span.ID, kind, 0), ExecutionID: span.ExecutionID, SpanID: span.ID, EventType: kind, Timestamp: ts, Metadata: map[string]string{"span_type": string(span.SpanType), "span_name": span.Name, "status": string(span.Status)}, Payload: &execution.PayloadRef{Inline: span, ContentType: "application/json"}})
}

// InsertEvent checks the relationship even on retries, and never rewrites history.
func (s *Store) InsertEvent(ctx context.Context, event *execution.ExecutionEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = insertEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func insertEventTx(ctx context.Context, tx pgx.Tx, event *execution.ExecutionEvent) error {
	event.Timestamp = event.Timestamp.UTC().Truncate(time.Microsecond)
	if event.Payload != nil && event.Payload.ContentType == "" {
		event.Payload.ContentType = "application/json"
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	var owned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM spans WHERE id=$1 AND execution_id=$2)`, event.SpanID, event.ExecutionID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return apperrors.InvalidInput("event span must belong to its execution")
	}
	md, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	var payload any
	if event.Payload != nil && event.Payload.Inline != nil {
		b, e := json.Marshal(event.Payload.Inline)
		if e != nil {
			return e
		}
		payload = string(b)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO execution_events(id,execution_id,span_id,event_type,payload_inline,payload_size,content_type,timestamp,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`, event.ID, event.ExecutionID, event.SpanID, event.EventType, payload, payloadSizeOrZero(event.Payload), payloadContentType(event.Payload), event.Timestamp, string(md))
	if err != nil {
		return apperrors.Internal("inserting event", err)
	}
	if tag.RowsAffected() == 0 {
		old, e := scanEvent(tx.QueryRow(ctx, `SELECT id,execution_id,span_id,event_type,payload_ref,payload_inline,payload_size,content_type,timestamp,metadata,sequence_number FROM execution_events WHERE id=$1`, event.ID))
		if e != nil {
			return e
		}
		old.SequenceNumber = 0
		cp := *event
		cp.SequenceNumber = 0
		// Empty payloads are represented by NULL in storage.
		if old.Payload != nil && old.Payload.Inline == nil && old.Payload.SizeBytes == 0 {
			old.Payload = nil
		}
		if !equalJSON(old, &cp) {
			return apperrors.AlreadyExists("event with different content", event.ID)
		}
	}
	return nil
}
