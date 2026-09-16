// Package lineage provides DAG construction and causality tracking.
package lineage

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

// Service handles lineage tracking and DAG construction.
type Service struct {
	store  *postgres.Store
	logger zerolog.Logger
}

// NewService creates a new lineage service.
func NewService(store *postgres.Store, logger zerolog.Logger) *Service {
	return &Service{
		store:  store,
		logger: logger.With().Str("module", "lineage").Logger(),
	}
}

// BuildDAG reconstructs the execution DAG from span relationships.
func (s *Service) BuildDAG(ctx context.Context, executionID string) ([]execution.DAGNode, []execution.DAGEdge, error) {
	spans, err := s.store.GetSpansByExecution(ctx, executionID)
	if err != nil {
		return nil, nil, err
	}

	nodes := make([]execution.DAGNode, 0, len(spans))
	edges := make([]execution.DAGEdge, 0)

	for _, span := range spans {
		node := execution.DAGNode{
			ID:        span.ID,
			SpanID:    span.ID,
			Name:      span.Name,
			SpanType:  span.SpanType,
			Status:    span.Status,
			StartedAt: span.StartedAt,
			EndedAt:   span.EndedAt,
			Duration:  span.DurationMs(),
			Metadata:  span.Metadata,
		}
		nodes = append(nodes, node)

		// Parent-child edge
		if span.ParentSpanID != nil && *span.ParentSpanID != "" {
			edges = append(edges, execution.DAGEdge{
				SourceSpanID: *span.ParentSpanID,
				TargetSpanID: span.ID,
				EdgeType:     "parent_child",
			})
		}

		// Causal edges
		for _, causedBy := range span.CausedBySpanIDs {
			edges = append(edges, execution.DAGEdge{
				SourceSpanID: causedBy,
				TargetSpanID: span.ID,
				EdgeType:     "causal",
			})
		}
	}

	s.logger.Debug().
		Str("execution_id", executionID).
		Int("nodes", len(nodes)).
		Int("edges", len(edges)).
		Msg("DAG reconstructed")

	return nodes, edges, nil
}

// GetCausalityChain returns the chain of spans that caused the given span.
func (s *Service) GetCausalityChain(ctx context.Context, spanID string) ([]*execution.Span, error) {
	span, err := s.store.GetSpan(ctx, spanID)
	if err != nil {
		return nil, err
	}

	chain := []*execution.Span{span}

	// Walk up the parent chain
	current := span
	for current.ParentSpanID != nil && *current.ParentSpanID != "" {
		parent, err := s.store.GetSpan(ctx, *current.ParentSpanID)
		if err != nil {
			break
		}
		chain = append([]*execution.Span{parent}, chain...)
		current = parent
	}

	return chain, nil
}
