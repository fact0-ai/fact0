export interface TimeSeriesPoint {
  bucket: string;
  value: number;
}

export interface TimeSeriesTokens {
  bucket: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ModelBreakdown {
  model_name: string;
  model_provider: string;
  call_count: number;
  avg_latency_ms: number;
  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  error_count: number;
  estimated_cost_usd: number;
}

export interface LLMMetrics {
  total_calls: number;
  success_count: number;
  error_count: number;
  success_rate: number;
  avg_latency_ms: number;
  p50_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
  by_model?: ModelBreakdown[];
  time_series?: TimeSeriesPoint[];
  token_time_series?: TimeSeriesTokens[];
}

export interface ToolBreakdown {
  tool_name: string;
  call_count: number;
  avg_duration_ms: number;
  error_count: number;
  success_rate: number;
}

export interface ToolMetrics {
  total_calls: number;
  success_count: number;
  error_count: number;
  success_rate: number;
  avg_duration_ms: number;
  by_tool?: ToolBreakdown[];
  time_series?: TimeSeriesPoint[];
}

export interface ErrorGroup {
  name: string;
  error_count: number;
  span_type: string;
}

export interface ErrorBreakdown {
  total_errors: number;
  by_model?: ErrorGroup[];
  by_tool?: ErrorGroup[];
  time_series?: TimeSeriesPoint[];
}

export interface ConversationSession {
  session_id: string;
  tenant_id: string;
  agent_id: string;
  agent_name: string;
  started_at: string;
  last_active_at: string;
  turn_count: number;
  total_tokens: number;
  total_cost_usd: number;
  status: string;
}

export interface LLMCallSummary {
  span_id: string;
  model_name: string;
  total_tokens: number;
  latency_ms: number;
  status: string;
}

export interface ToolCallSummary {
  span_id: string;
  tool_name: string;
  duration_ms: number;
  status: string;
}

export interface TurnTrace {
  execution_id: string;
  sequence: number;
  agent_id: string;
  agent_name: string;
  status: string;
  started_at: string;
  ended_at?: string;
  llm_calls?: LLMCallSummary[];
  tool_calls?: ToolCallSummary[];
  total_tokens: number;
  total_cost_usd: number;
  duration_ms: number;
}

export interface SessionDetail {
  session: ConversationSession;
  turns: TurnTrace[];
}

export interface PromptRecord {
  id: string;
  tenant_id: string;
  name: string;
  version: number;
  template: string;
  variables?: string[];
  model_hints?: string[];
  metadata?: Record<string, string>;
  created_at: string;
  usage_count: number;
  avg_tokens: number;
  avg_latency_ms: number;
}
