// Package execution defines the core domain model for execution tracing.
// All types here represent the internal domain - distinct from protobuf wire types.
package execution

import (
	"time"
)

// Status represents the lifecycle state of an execution or span.
type Status string

const (
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

// SpanKind classifies the type of work a span represents.
type SpanKind string

const (
	SpanKindToolCall         SpanKind = "TOOL_CALL"
	SpanKindModelInvocation  SpanKind = "MODEL_INVOCATION"
	SpanKindStateMutation    SpanKind = "STATE_MUTATION"
	SpanKindHumanApproval    SpanKind = "HUMAN_APPROVAL"
	SpanKindPolicyEvaluation SpanKind = "POLICY_EVALUATION"
	SpanKindCustom           SpanKind = "CUSTOM"
)

// Execution is the top-level trace representing a single AI agent run.
type Execution struct {
	ID         string            `json:"id"`
	AgentID    string            `json:"agent_id"`
	AgentName  string            `json:"agent_name"`
	Status     Status            `json:"status"`
	RootSpanID string            `json:"root_span_id"`
	Trigger    string            `json:"trigger"`
	StartedAt  time.Time         `json:"started_at"`
	EndedAt    *time.Time        `json:"ended_at,omitempty"`
	Metadata   map[string]string `json:"metadata"`
	CreatedAt  time.Time         `json:"created_at"`
}

// Span is a unit of work within an execution.
type Span struct {
	ID              string            `json:"id"`
	ExecutionID     string            `json:"execution_id"`
	ParentSpanID    *string           `json:"parent_span_id,omitempty"`
	CausedBySpanIDs []string          `json:"caused_by_span_ids,omitempty"`
	SpanType        SpanKind          `json:"span_type"`
	Name            string            `json:"name"`
	Status          Status            `json:"status"`
	StartedAt       time.Time         `json:"started_at"`
	EndedAt         *time.Time        `json:"ended_at,omitempty"`
	Metadata        map[string]string `json:"metadata"`

	// Type-specific details
	ToolCall         *ToolCallDetail         `json:"tool_call,omitempty"`
	ModelInvocation  *ModelInvocationDetail  `json:"model_invocation,omitempty"`
	StateMutation    *StateMutationDetail    `json:"state_mutation,omitempty"`
	HumanApproval    *HumanApprovalDetail    `json:"human_approval,omitempty"`
	PolicyEvaluation *PolicyEvaluationDetail `json:"policy_evaluation,omitempty"`

	Error *SpanError `json:"error,omitempty"`
}

// DurationMs returns the span duration in milliseconds.
func (s *Span) DurationMs() int64 {
	if s.EndedAt == nil {
		return 0
	}
	return s.EndedAt.Sub(s.StartedAt).Milliseconds()
}

// SpanError captures failure details.
type SpanError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StackTrace string `json:"stack_trace,omitempty"`
}

// PayloadRef holds a payload stored inline in Postgres.
type PayloadRef struct {
	Inline      interface{} `json:"inline,omitempty"`
	SizeBytes   int64       `json:"size_bytes"`
	ContentType string      `json:"content_type,omitempty"`
}

// ToolCallDetail captures external tool invocation details.
type ToolCallDetail struct {
	ToolName    string      `json:"tool_name"`
	ToolVersion string      `json:"tool_version,omitempty"`
	Input       *PayloadRef `json:"input,omitempty"`
	Output      *PayloadRef `json:"output,omitempty"`
	DurationMs  int64       `json:"duration_ms"`
}

// ModelInvocationDetail captures LLM call details.
type ModelInvocationDetail struct {
	ModelName        string      `json:"model_name"`
	ModelProvider    string      `json:"model_provider"`
	Prompt           *PayloadRef `json:"prompt,omitempty"`
	Completion       *PayloadRef `json:"completion,omitempty"`
	PromptTokens     int32       `json:"prompt_tokens"`
	CompletionTokens int32       `json:"completion_tokens"`
	TotalTokens      int32       `json:"total_tokens"`
	LatencyMs        int64       `json:"latency_ms"`
	Temperature      float64     `json:"temperature"`
	SessionID        string      `json:"session_id,omitempty"`
	TurnSequence     int         `json:"turn_sequence,omitempty"`
	PromptName       string      `json:"prompt_name,omitempty"`
	PromptVersion    int         `json:"prompt_version,omitempty"`
	CostUSD          float64     `json:"cost_usd,omitempty"`
}

// StateMutationDetail captures state change events.
type StateMutationDetail struct {
	Key          string      `json:"key"`
	BeforeValue  *PayloadRef `json:"before_value,omitempty"`
	AfterValue   *PayloadRef `json:"after_value,omitempty"`
	MutationType string      `json:"mutation_type"`
}

// HumanApprovalDetail captures human-in-the-loop decisions.
type HumanApprovalDetail struct {
	ApproverID   string    `json:"approver_id"`
	ApproverName string    `json:"approver_name"`
	Decision     string    `json:"decision"`
	Reasoning    string    `json:"reasoning"`
	DecidedAt    time.Time `json:"decided_at"`
}

// PolicyEvaluationDetail captures governance policy checks.
type PolicyEvaluationDetail struct {
	PolicyID   string            `json:"policy_id"`
	PolicyName string            `json:"policy_name"`
	Result     string            `json:"result"`
	Violations []string          `json:"violations,omitempty"`
	Context    map[string]string `json:"context,omitempty"`
}

// ExecutionEvent is a single append-only event in the execution log.
type ExecutionEvent struct {
	ID             string            `json:"id"`
	ExecutionID    string            `json:"execution_id"`
	SpanID         string            `json:"span_id"`
	EventType      string            `json:"event_type"`
	Timestamp      time.Time         `json:"timestamp"`
	SequenceNumber int64             `json:"sequence_number"`
	Payload        *PayloadRef       `json:"payload,omitempty"`
	Metadata       map[string]string `json:"metadata"`
}

// DAGNode represents a span as a node in the execution graph.
type DAGNode struct {
	ID        string            `json:"id"`
	SpanID    string            `json:"span_id"`
	Name      string            `json:"name"`
	SpanType  SpanKind          `json:"span_type"`
	Status    Status            `json:"status"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
	Duration  int64             `json:"duration_ms"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// DAGEdge represents a relationship between two spans.
type DAGEdge struct {
	SourceSpanID string `json:"source"`
	TargetSpanID string `json:"target"`
	EdgeType     string `json:"edge_type"` // "parent_child" or "causal"
}

// ReplayFrame is a single step in execution replay.
type ReplayFrame struct {
	SequenceNumber int64           `json:"sequence_number"`
	EventType      string          `json:"event_type"`
	SpanID         string          `json:"span_id"`
	SpanName       string          `json:"span_name"`
	SpanType       SpanKind        `json:"span_type"`
	Event          *ExecutionEvent `json:"event"`
	DeltaMs        int64           `json:"delta_ms"`
	ElapsedMs      int64           `json:"elapsed_ms"`
}
