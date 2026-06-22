package fact0

import "context"

// StartExecutionRequest starts a new execution trace.
type StartExecutionRequest struct {
	AgentID        string            `json:"agent_id"`
	AgentName      string            `json:"agent_name,omitempty"`
	Trigger        string            `json:"trigger,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

// IngestSpansRequest represents span payload for ingestion.
type IngestSpansRequest struct {
	Spans []map[string]any `json:"spans"`
}

// IngestEventsRequest represents event payload for ingestion.
type IngestEventsRequest struct {
	Events []map[string]any `json:"events"`
}

// ModelInvocationDetail represents model details in a span.
type ModelInvocationDetail struct {
	ModelName        string   `json:"model_name"`
	ModelProvider    string   `json:"model_provider"`
	PromptTokens     int32    `json:"prompt_tokens,omitempty"`
	CompletionTokens int32    `json:"completion_tokens,omitempty"`
	TotalTokens      int32    `json:"total_tokens,omitempty"`
	LatencyMs        int64    `json:"latency_ms,omitempty"`
	Temperature      float64  `json:"temperature,omitempty"`
	SessionID        string   `json:"session_id,omitempty"`
	TurnSequence     int32    `json:"turn_sequence,omitempty"`
	PromptName       string   `json:"prompt_name,omitempty"`
	PromptVersion    int32    `json:"prompt_version,omitempty"`
	CostUSD          float64  `json:"cost_usd,omitempty"`
}

// TelemetryClient wraps execution telemetry REST endpoints.
type TelemetryClient struct {
	parent *Client
}

// StartExecution creates a new execution.
func (t *TelemetryClient) StartExecution(ctx context.Context, req StartExecutionRequest) (map[string]any, error) {
	var out map[string]any
	err := t.parent.doJSON(ctx, "POST", "/api/v1/executions", req, &out, true)
	return out, err
}

// EndExecution marks an execution complete.
func (t *TelemetryClient) EndExecution(ctx context.Context, executionID, status string) (map[string]any, error) {
	var out map[string]any
	err := t.parent.doJSON(ctx, "PUT", "/api/v1/executions/"+executionID+"/end", map[string]string{"status": status}, &out, true)
	return out, err
}

// IngestSpans ingests execution trace spans.
func (t *TelemetryClient) IngestSpans(ctx context.Context, executionID string, spans []map[string]any) (map[string]any, error) {
	var out map[string]any
	req := IngestSpansRequest{Spans: spans}
	err := t.parent.doJSON(ctx, "POST", "/api/v1/executions/"+executionID+"/spans", req, &out, true)
	return out, err
}

// IngestEvents ingests execution trace events.
func (t *TelemetryClient) IngestEvents(ctx context.Context, executionID string, events []map[string]any) (map[string]any, error) {
	var out map[string]any
	req := IngestEventsRequest{Events: events}
	err := t.parent.doJSON(ctx, "POST", "/api/v1/executions/"+executionID+"/events", req, &out, true)
	return out, err
}

// GetExecution returns execution detail.
func (t *TelemetryClient) GetExecution(ctx context.Context, executionID string) (map[string]any, error) {
	var out map[string]any
	err := t.parent.doJSON(ctx, "GET", "/api/v1/executions/"+executionID, nil, &out, true)
	return out, err
}

// ListExecutions lists executions.
func (t *TelemetryClient) ListExecutions(ctx context.Context, query string) (map[string]any, error) {
	var out map[string]any
	err := t.parent.doJSON(ctx, "GET", "/api/v1/executions"+query, nil, &out, true)
	return out, err
}

// GetDAG returns the execution DAG.
func (t *TelemetryClient) GetDAG(ctx context.Context, executionID string) (map[string]any, error) {
	var out map[string]any
	err := t.parent.doJSON(ctx, "GET", "/api/v1/executions/"+executionID+"/dag", nil, &out, true)
	return out, err
}

// Replay returns replay frames.
func (t *TelemetryClient) Replay(ctx context.Context, executionID, query string) (map[string]any, error) {
	var out map[string]any
	err := t.parent.doJSON(ctx, "GET", "/api/v1/executions/"+executionID+"/replay"+query, nil, &out, true)
	return out, err
}
