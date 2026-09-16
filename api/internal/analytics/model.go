// Package analytics provides aggregated observability metrics for
// AI agent executions, including LLM call analytics, tool call
// analytics, token usage, cost tracking, and prompt management.
package analytics

import "time"

// ─── Time-series / aggregate types ──────────────────────────

// TimeSeriesPoint represents a single data point in a time-bucketed series.
type TimeSeriesPoint struct {
	Bucket string  `json:"bucket"`
	Value  float64 `json:"value"`
}

// TimeSeriesTokens represents a time-bucketed token breakdown.
type TimeSeriesTokens struct {
	Bucket           string `json:"bucket"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
}

// TimeSeriesCost represents a time-bucketed cost data point.
type TimeSeriesCost struct {
	Bucket       string  `json:"bucket"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	TotalTokens  int64   `json:"total_tokens"`
	CallCount    int64   `json:"call_count"`
}

// ─── LLM call analytics ─────────────────────────────────────

// LLMMetrics contains aggregated LLM call statistics.
type LLMMetrics struct {
	TotalCalls            int64              `json:"total_calls"`
	SuccessCount          int64              `json:"success_count"`
	ErrorCount            int64              `json:"error_count"`
	SuccessRate           float64            `json:"success_rate"`
	AvgLatencyMs          float64            `json:"avg_latency_ms"`
	P50LatencyMs          float64            `json:"p50_latency_ms"`
	P95LatencyMs          float64            `json:"p95_latency_ms"`
	P99LatencyMs          float64            `json:"p99_latency_ms"`
	TotalPromptTokens     int64              `json:"total_prompt_tokens"`
	TotalCompletionTokens int64              `json:"total_completion_tokens"`
	TotalTokens           int64              `json:"total_tokens"`
	EstimatedCostUSD      float64            `json:"estimated_cost_usd"`
	ByModel               []*ModelBreakdown  `json:"by_model,omitempty"`
	TimeSeries            []TimeSeriesPoint  `json:"time_series,omitempty"`
	TokenTimeSeries       []TimeSeriesTokens `json:"token_time_series,omitempty"`
}

// ModelBreakdown shows per-model LLM usage statistics.
type ModelBreakdown struct {
	ModelName        string  `json:"model_name"`
	ModelProvider    string  `json:"model_provider"`
	CallCount        int64   `json:"call_count"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	TotalTokens      int64   `json:"total_tokens"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ErrorCount       int64   `json:"error_count"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// LLMCallRow represents a single LLM invocation from the spans table.
type LLMCallRow struct {
	SpanID           string     `json:"span_id"`
	ExecutionID      string     `json:"execution_id"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	ModelName        string     `json:"model_name"`
	ModelProvider    string     `json:"model_provider"`
	PromptTokens     int32      `json:"prompt_tokens"`
	CompletionTokens int32      `json:"completion_tokens"`
	TotalTokens      int32      `json:"total_tokens"`
	LatencyMs        int64      `json:"latency_ms"`
	Temperature      float64    `json:"temperature"`
	EstimatedCostUSD float64    `json:"estimated_cost_usd"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	HasError         bool       `json:"has_error"`
	ErrorMessage     string     `json:"error_message,omitempty"`
}

// ─── Tool call analytics ─────────────────────────────────────

// ToolMetrics contains aggregated tool call statistics.
type ToolMetrics struct {
	TotalCalls   int64              `json:"total_calls"`
	SuccessCount int64              `json:"success_count"`
	ErrorCount   int64              `json:"error_count"`
	SuccessRate  float64            `json:"success_rate"`
	AvgDurationMs float64           `json:"avg_duration_ms"`
	ByTool       []*ToolBreakdown   `json:"by_tool,omitempty"`
	TimeSeries   []TimeSeriesPoint  `json:"time_series,omitempty"`
}

// ToolBreakdown shows per-tool usage statistics.
type ToolBreakdown struct {
	ToolName      string  `json:"tool_name"`
	CallCount     int64   `json:"call_count"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	ErrorCount    int64   `json:"error_count"`
	SuccessRate   float64 `json:"success_rate"`
}

// ToolCallRow represents a single tool invocation from the spans table.
type ToolCallRow struct {
	SpanID      string     `json:"span_id"`
	ExecutionID string     `json:"execution_id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	ToolName    string     `json:"tool_name"`
	DurationMs  int64      `json:"duration_ms"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	HasError    bool       `json:"has_error"`
	ErrorMessage string   `json:"error_message,omitempty"`
}

// ─── Error analytics ─────────────────────────────────────────

// ErrorBreakdown shows errors grouped by source.
type ErrorBreakdown struct {
	TotalErrors int64             `json:"total_errors"`
	ByModel     []*ErrorGroup     `json:"by_model,omitempty"`
	ByTool      []*ErrorGroup     `json:"by_tool,omitempty"`
	TimeSeries  []TimeSeriesPoint `json:"time_series,omitempty"`
}

// ErrorGroup is a group of errors by a specific entity.
type ErrorGroup struct {
	Name       string `json:"name"`
	ErrorCount int64  `json:"error_count"`
	SpanType   string `json:"span_type"`
}

// ─── Conversation / session analytics ────────────────────────

// ConversationSession represents a grouped session of agent executions.
type ConversationSession struct {
	SessionID    string    `json:"session_id"`
	TenantID     string    `json:"tenant_id"`
	AgentID      string    `json:"agent_id"`
	AgentName    string    `json:"agent_name"`
	StartedAt    time.Time `json:"started_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	TurnCount    int       `json:"turn_count"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCostUSD float64   `json:"total_cost_usd"`
	Status       string    `json:"status"`
}

// SessionDetail includes a session with all its turns.
type SessionDetail struct {
	Session ConversationSession `json:"session"`
	Turns   []TurnTrace         `json:"turns"`
}

// TurnTrace represents a single turn within a conversation.
type TurnTrace struct {
	ExecutionID  string          `json:"execution_id"`
	Sequence     int             `json:"sequence"`
	AgentID      string          `json:"agent_id"`
	AgentName    string          `json:"agent_name"`
	Status       string          `json:"status"`
	StartedAt    time.Time       `json:"started_at"`
	EndedAt      *time.Time      `json:"ended_at,omitempty"`
	LLMCalls     []LLMCallSummary  `json:"llm_calls"`
	ToolCalls    []ToolCallSummary `json:"tool_calls"`
	TotalTokens  int64           `json:"total_tokens"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	DurationMs   int64           `json:"duration_ms"`
}

// LLMCallSummary is a lightweight LLM call reference within a turn.
type LLMCallSummary struct {
	SpanID        string `json:"span_id"`
	ModelName     string `json:"model_name"`
	TotalTokens   int32  `json:"total_tokens"`
	LatencyMs     int64  `json:"latency_ms"`
	Status        string `json:"status"`
}

// ToolCallSummary is a lightweight tool call reference within a turn.
type ToolCallSummary struct {
	SpanID     string `json:"span_id"`
	ToolName   string `json:"tool_name"`
	DurationMs int64  `json:"duration_ms"`
	Status     string `json:"status"`
}

// ─── Prompt catalog ──────────────────────────────────────────

// PromptRecord represents a versioned prompt template.
type PromptRecord struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	Name      string            `json:"name"`
	Version   int               `json:"version"`
	Template  string            `json:"template"`
	Variables []string          `json:"variables,omitempty"`
	ModelHints []string         `json:"model_hints,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	// Computed fields (populated by analytics queries)
	UsageCount   int64   `json:"usage_count"`
	AvgTokens    float64 `json:"avg_tokens"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// ─── Query filter types ──────────────────────────────────────

// TimeRange defines a time window for analytics queries.
type TimeRange struct {
	From     time.Time
	To       time.Time
	Interval string // "1h", "1d", "1w"
}

// AnalyticsFilter is the common filter for analytics queries.
type AnalyticsFilter struct {
	TenantID      string
	TimeRange     TimeRange
	ModelName     string
	ModelProvider string
	ToolName      string
	AgentID       string
	GroupBy       string // "model", "provider", "day", "hour"
	Limit         int
	Offset        int
}
