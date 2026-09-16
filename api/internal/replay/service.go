// Package replay provides execution replay and DAG reconstruction.
package replay

import (
	"context"
	"sort"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

// Service handles execution replay.
type Service struct {
	store  *postgres.Store
	logger zerolog.Logger
}

// NewService creates a new replay service.
func NewService(store *postgres.Store, logger zerolog.Logger) *Service {
	return &Service{
		store:  store,
		logger: logger.With().Str("module", "replay").Logger(),
	}
}

// ReplayExecution reconstructs an ordered replay of an execution.
func (s *Service) ReplayExecution(ctx context.Context, tenantID string, req ReplayRequest) (*ReplayResponse, error) {
	exec, err := s.store.GetExecution(ctx, tenantID, req.ExecutionID)
	if err != nil {
		return nil, err
	}

	events, err := s.store.GetEventsByExecution(ctx, req.ExecutionID)
	if err != nil {
		return nil, err
	}

	// Build a span lookup for enrichment
	spans, err := s.store.GetSpansByExecution(ctx, req.ExecutionID)
	if err != nil {
		return nil, err
	}
	spanMap := make(map[string]*execution.Span, len(spans))
	for _, sp := range spans {
		spanMap[sp.ID] = sp
	}

	// Filter events if span IDs specified
	if len(req.SpanIDs) > 0 {
		spanFilter := make(map[string]bool, len(req.SpanIDs))
		for _, id := range req.SpanIDs {
			spanFilter[id] = true
		}
		var filtered []*execution.ExecutionEvent
		for _, evt := range events {
			if spanFilter[evt.SpanID] {
				filtered = append(filtered, evt)
			}
		}
		events = filtered
	}

	// Filter by sequence range
	if req.FromSequence > 0 || req.ToSequence > 0 {
		var filtered []*execution.ExecutionEvent
		for _, evt := range events {
			if req.FromSequence > 0 && evt.SequenceNumber < req.FromSequence {
				continue
			}
			if req.ToSequence > 0 && evt.SequenceNumber > req.ToSequence {
				continue
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}

	// Replay follows recorded time; ingestion sequence remains as a stable tie breaker.
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].SequenceNumber < events[j].SequenceNumber
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	// Build replay frames
	frames := make([]execution.ReplayFrame, 0, len(events))
	var prevTimestamp = exec.StartedAt

	for _, evt := range events {
		span := spanMap[evt.SpanID]
		frame := execution.ReplayFrame{
			SequenceNumber: evt.SequenceNumber,
			EventType:      evt.EventType,
			SpanID:         evt.SpanID,
			Event:          evt,
			DeltaMs:        evt.Timestamp.Sub(prevTimestamp).Milliseconds(),
			ElapsedMs:      evt.Timestamp.Sub(exec.StartedAt).Milliseconds(),
		}
		if span != nil {
			frame.SpanName = span.Name
			frame.SpanType = span.SpanType
		}
		if frame.DeltaMs < 0 {
			frame.DeltaMs = 0
		}
		if frame.ElapsedMs < 0 {
			frame.ElapsedMs = 0
		}
		frames = append(frames, frame)
		prevTimestamp = evt.Timestamp
	}

	s.logger.Info().
		Str("execution_id", req.ExecutionID).
		Int("frames", len(frames)).
		Msg("replay reconstructed")

	duration := int64(0)
	if exec.EndedAt != nil {
		duration = exec.EndedAt.Sub(exec.StartedAt).Milliseconds()
	}
	if len(frames) > 0 && frames[len(frames)-1].ElapsedMs > duration {
		duration = frames[len(frames)-1].ElapsedMs
	}
	if duration < 0 {
		duration = 0
	}
	return &ReplayResponse{
		DurationMs:  duration,
		Execution:   exec,
		Frames:      frames,
		TotalFrames: int64(len(frames)),
	}, nil
}

// ─── Request/Response Types ─────────────────────────────────

// ReplayRequest specifies replay parameters.
type ReplayRequest struct {
	ExecutionID  string   `json:"execution_id"`
	FromSequence int64    `json:"from_sequence,omitempty"`
	ToSequence   int64    `json:"to_sequence,omitempty"`
	SpanIDs      []string `json:"span_ids,omitempty"`
}

// ReplayResponse contains the ordered replay frames.
type ReplayResponse struct {
	DurationMs  int64                   `json:"duration_ms"`
	Execution   *execution.Execution    `json:"execution"`
	Frames      []execution.ReplayFrame `json:"frames"`
	TotalFrames int64                   `json:"total_frames"`
}
