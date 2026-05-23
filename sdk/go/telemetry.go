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
