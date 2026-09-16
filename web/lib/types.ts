// Execution domain types - mirrors the Go/protobuf domain model

export type ExecutionStatus = "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED";
export type SpanStatus = "STARTED" | "COMPLETED" | "FAILED" | "CANCELLED";
export type SpanKind =
  | "TOOL_CALL"
  | "MODEL_INVOCATION"
  | "STATE_MUTATION"
  | "HUMAN_APPROVAL"
  | "POLICY_EVALUATION"
  | "CUSTOM";

export interface Execution {
  id: string;
  agent_id: string;
  agent_name: string;
  status: ExecutionStatus;
  root_span_id: string;
  trigger: string;
  started_at: string;
  ended_at?: string;
  metadata: Record<string, string>;
  created_at: string;
}

export interface Span {
  id: string;
  execution_id: string;
  parent_span_id?: string;
  caused_by_span_ids?: string[];
  span_type: SpanKind;
  name: string;
  status: SpanStatus;
  started_at: string;
  ended_at?: string;
  metadata: Record<string, string>;
  tool_call?: ToolCallDetail;
  model_invocation?: ModelInvocationDetail;
  state_mutation?: StateMutationDetail;
  human_approval?: HumanApprovalDetail;
  policy_evaluation?: PolicyEvaluationDetail;
  error?: SpanError;
}

export interface SpanError {
  code: string;
  message: string;
  stack_trace?: string;
}

export interface PayloadRef {
  inline?: unknown;
  blob_ref?: string;
  size_bytes: number;
  content_type?: string;
}

export interface ToolCallDetail {
  tool_name: string;
  tool_version?: string;
  input?: PayloadRef;
  output?: PayloadRef;
  duration_ms: number;
}

export interface ModelInvocationDetail {
  model_name: string;
  model_provider: string;
  prompt?: PayloadRef;
  completion?: PayloadRef;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  latency_ms: number;
  temperature?: number;
  session_id?: string;
  turn_sequence?: number;
  prompt_name?: string;
  prompt_version?: number;
  cost_usd?: number;
}

export interface StateMutationDetail {
  key: string;
  before_value?: PayloadRef;
  after_value?: PayloadRef;
  mutation_type: string;
}

export interface HumanApprovalDetail {
  approver_id: string;
  approver_name: string;
  decision: string;
  reasoning: string;
  decided_at: string;
}

export interface PolicyEvaluationDetail {
  policy_id: string;
  policy_name: string;
  result: string;
  violations?: string[];
  context?: Record<string, string>;
}

export interface ExecutionEvent {
  id: string;
  execution_id: string;
  span_id: string;
  event_type: string;
  timestamp: string;
  sequence_number: number;
  payload?: PayloadRef;
  metadata: Record<string, string>;
}

export interface DAGNode {
  id: string;
  span_id: string;
  name: string;
  span_type: SpanKind;
  status: SpanStatus;
  started_at: string;
  ended_at?: string;
  duration_ms: number;
  metadata?: Record<string, string>;
}

export interface DAGEdge {
  source: string;
  target: string;
  edge_type: "parent_child" | "causal";
}

export interface ReplayFrame {
  sequence_number: number;
  event_type: string;
  span_id: string;
  span_name: string;
  span_type: SpanKind;
  event: ExecutionEvent;
  delta_ms: number;
  elapsed_ms: number;
}

export interface ReplayResponse {
  /** Recorded duration from the API, including the execution end. */
  duration_ms?: number;
  execution: Execution;
  frames: ReplayFrame[];
  total_frames: number;
}
