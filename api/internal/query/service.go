// Package query provides read access to execution data.
package query

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/execution"
	"github.com/fact0-ai/fact0/internal/lineage"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
)

// Service handles execution queries.
type Service struct {
	store   *postgres.Store
	lineage *lineage.Service
	logger  zerolog.Logger
}

// NewService creates a new query service.
func NewService(store *postgres.Store, lineageSvc *lineage.Service, logger zerolog.Logger) *Service {
	return &Service{
		store:   store,
		lineage: lineageSvc,
		logger:  logger.With().Str("module", "query").Logger(),
	}
}

// GetExecution retrieves a single execution with metadata.
func (s *Service) GetExecution(ctx context.Context, tenantID, executionID string) (*execution.Execution, error) {
	return s.store.GetExecution(ctx, tenantID, executionID)
}

// ListExecutions returns paginated execution summaries.
func (s *Service) ListExecutions(ctx context.Context, tenantID string, req ListExecutionsRequest) (*ListExecutionsResponse, error) {
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	executions, total, err := s.store.ListExecutions(ctx, tenantID, req.AgentID, req.Status, req.PageSize, req.Offset)
	if err != nil {
		return nil, err
	}

	return &ListExecutionsResponse{
		Executions: executions,
		Total:      total,
		PageSize:   req.PageSize,
		Offset:     req.Offset,
	}, nil
}

// GetSpan retrieves a single span with its events.
func (s *Service) GetSpan(ctx context.Context, tenantID, spanID string) (*execution.Span, []*execution.ExecutionEvent, error) {
	span, err := s.store.GetSpan(ctx, spanID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.store.GetExecution(ctx, tenantID, span.ExecutionID); err != nil {
		return nil, nil, err
	}

	events, err := s.store.GetEventsBySpan(ctx, spanID)
	if err != nil {
		return nil, nil, err
	}

	return span, events, nil
}

// GetSpansByExecution returns all spans for an execution.
func (s *Service) GetSpansByExecution(ctx context.Context, tenantID, executionID string) ([]*execution.Span, error) {
	if _, err := s.store.GetExecution(ctx, tenantID, executionID); err != nil {
		return nil, err
	}
	return s.store.GetSpansByExecution(ctx, executionID)
}

// GetExecutionDAG returns the reconstructed DAG.
func (s *Service) GetExecutionDAG(ctx context.Context, tenantID, executionID string) ([]execution.DAGNode, []execution.DAGEdge, error) {
	if _, err := s.store.GetExecution(ctx, tenantID, executionID); err != nil {
		return nil, nil, err
	}
	return s.lineage.BuildDAG(ctx, executionID)
}

// GetSpanEvents returns events for a specific span.
func (s *Service) GetSpanEvents(ctx context.Context, tenantID, spanID string) ([]*execution.ExecutionEvent, error) {
	span, err := s.store.GetSpan(ctx, spanID)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetExecution(ctx, tenantID, span.ExecutionID); err != nil {
		return nil, err
	}
	return s.store.GetEventsBySpan(ctx, spanID)
}

// ─── Request/Response Types ─────────────────────────────────

// ListExecutionsRequest holds pagination and filter parameters.
type ListExecutionsRequest struct {
	PageSize int    `json:"page_size"`
	Offset   int    `json:"offset"`
	AgentID  string `json:"agent_id"`
	Status   string `json:"status"`
}

// ListExecutionsResponse holds paginated results.
type ListExecutionsResponse struct {
	Executions []*execution.Execution `json:"executions"`
	Total      int                    `json:"total"`
	PageSize   int                    `json:"page_size"`
	Offset     int                    `json:"offset"`
}
