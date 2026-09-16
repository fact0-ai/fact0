package analytics

import (
	"context"
	cryptorand "crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(cryptorand.Reader, 0)
)

func newPromptID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "pmpt_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// Store defines the database storage interface required by the analytics service.
type Store interface {
	GetLLMMetrics(ctx context.Context, tenantID string, from, to time.Time) (*LLMMetrics, error)
	GetToolMetrics(ctx context.Context, tenantID string, from, to time.Time) (*ToolMetrics, error)
	GetErrorMetrics(ctx context.Context, tenantID string, from, to time.Time) (*ErrorBreakdown, error)
	ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]ConversationSession, error)
	GetSessionDetail(ctx context.Context, tenantID, sessionID string) (*SessionDetail, error)
	InsertPrompt(ctx context.Context, p *PromptRecord) error
	GetPromptLatestVersion(ctx context.Context, tenantID, name string) (int, error)
	ListPrompts(ctx context.Context, tenantID string) ([]PromptRecord, error)
	GetPrompt(ctx context.Context, tenantID, id string) (*PromptRecord, error)
}

// Service coordinates observability telemetry aggregations and prompt management.
type Service struct {
	store Store
}

// NewService instantiates the analytics business service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// GetLLMMetrics retrieves tenant-scoped LLM aggregates.
func (s *Service) GetLLMMetrics(ctx context.Context, tenantID string, from, to time.Time) (*LLMMetrics, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenantID cannot be empty")
	}
	if from.IsZero() || to.IsZero() {
		to = time.Now()
		from = to.Add(-30 * 24 * time.Hour) // default 30d
	}
	return s.store.GetLLMMetrics(ctx, tenantID, from, to)
}

// GetToolMetrics retrieves tenant-scoped tool usage statistics.
func (s *Service) GetToolMetrics(ctx context.Context, tenantID string, from, to time.Time) (*ToolMetrics, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenantID cannot be empty")
	}
	if from.IsZero() || to.IsZero() {
		to = time.Now()
		from = to.Add(-30 * 24 * time.Hour)
	}
	return s.store.GetToolMetrics(ctx, tenantID, from, to)
}

// GetErrorMetrics retrieves tenant-scoped error frequency breakdown.
func (s *Service) GetErrorMetrics(ctx context.Context, tenantID string, from, to time.Time) (*ErrorBreakdown, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenantID cannot be empty")
	}
	if from.IsZero() || to.IsZero() {
		to = time.Now()
		from = to.Add(-30 * 24 * time.Hour)
	}
	return s.store.GetErrorMetrics(ctx, tenantID, from, to)
}

// ListSessions retrieves paginated conversation sessions.
func (s *Service) ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]ConversationSession, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenantID cannot be empty")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListSessions(ctx, tenantID, limit, offset)
}

// GetSessionDetail retrieves full session metadata, including its turns and nested calls.
func (s *Service) GetSessionDetail(ctx context.Context, tenantID, sessionID string) (*SessionDetail, error) {
	if tenantID == "" || sessionID == "" {
		return nil, apperrors.InvalidInput("tenantID and sessionID cannot be empty")
	}
	return s.store.GetSessionDetail(ctx, tenantID, sessionID)
}

// CreatePrompt creates a new prompt or registers a new version of an existing prompt template.
func (s *Service) CreatePrompt(
	ctx context.Context,
	tenantID string,
	name string,
	template string,
	variables []string,
	modelHints []string,
	metadata map[string]string,
) (*PromptRecord, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenantID cannot be empty")
	}
	if name == "" {
		return nil, apperrors.InvalidInput("prompt name cannot be empty")
	}
	if template == "" {
		return nil, apperrors.InvalidInput("prompt template cannot be empty")
	}

	// Determine version: fetch current highest version and increment.
	latestVer, err := s.store.GetPromptLatestVersion(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}

	prompt := &PromptRecord{
		ID:         newPromptID(),
		TenantID:   tenantID,
		Name:       name,
		Version:    latestVer + 1,
		Template:   template,
		Variables:  variables,
		ModelHints: modelHints,
		Metadata:   metadata,
		CreatedAt:  time.Now().UTC(),
	}

	if err := s.store.InsertPrompt(ctx, prompt); err != nil {
		return nil, err
	}

	return prompt, nil
}

// ListPrompts lists all prompts (with metadata & average performance counters).
func (s *Service) ListPrompts(ctx context.Context, tenantID string) ([]PromptRecord, error) {
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenantID cannot be empty")
	}
	return s.store.ListPrompts(ctx, tenantID)
}

// GetPrompt retrieves a specific prompt configuration.
func (s *Service) GetPrompt(ctx context.Context, tenantID, id string) (*PromptRecord, error) {
	if tenantID == "" || id == "" {
		return nil, apperrors.InvalidInput("tenantID and prompt ID cannot be empty")
	}
	return s.store.GetPrompt(ctx, tenantID, id)
}
