"use client";

import { useMemo } from "react";
import useSWR from "swr";
import useSWRMutation from "swr/mutation";

import { authClient } from "./auth-client";
import { useDemoMode } from "./demo/demo-context";
import { meClient } from "./me-api";
import { fetchToken } from "./use-me";
import {
  LLMMetrics,
  ToolMetrics,
  ErrorBreakdown,
  ConversationSession,
  SessionDetail,
  PromptRecord
} from "./analytics-types";

function useMeClient() {
  const { data: session, isPending: sessionPending } = authClient.useSession();
  const { data: activeOrg, isPending: activeOrgPending } =
    authClient.useActiveOrganization();

  const client = useMemo(() => meClient(fetchToken), []);

  const authReady = !sessionPending && !!session?.user;
  const orgHydrated = !activeOrgPending;
  const orgId = activeOrg?.id ?? null;
  const ready = authReady && orgHydrated && orgId !== null && orgId.length > 0;

  return { client, ready, orgId };
}

// ─── DEMO FIXTURES ──────────────────────────────────────────

const DEMO_LLM: LLMMetrics = {
  total_calls: 15480,
  success_count: 15325,
  error_count: 155,
  success_rate: 99.0,
  avg_latency_ms: 850,
  p50_latency_ms: 620,
  p95_latency_ms: 1850,
  p99_latency_ms: 3200,
  total_prompt_tokens: 42850900,
  total_completion_tokens: 12560800,
  total_tokens: 55411700,
  estimated_cost_usd: 142.85,
  by_model: [
    {
      model_name: "claude-3-5-sonnet",
      model_provider: "anthropic",
      call_count: 8200,
      avg_latency_ms: 1100,
      total_tokens: 35000000,
      prompt_tokens: 28000000,
      completion_tokens: 7000000,
      error_count: 45,
      estimated_cost_usd: 105.0,
    },
    {
      model_name: "gpt-4o",
      model_provider: "openai",
      call_count: 5200,
      avg_latency_ms: 650,
      total_tokens: 15000000,
      prompt_tokens: 11000000,
      completion_tokens: 4000000,
      error_count: 85,
      estimated_cost_usd: 35.0,
    },
    {
      model_name: "gemini-1.5-pro",
      model_provider: "google",
      call_count: 2080,
      avg_latency_ms: 450,
      total_tokens: 5411700,
      prompt_tokens: 3850900,
      completion_tokens: 1560800,
      error_count: 25,
      estimated_cost_usd: 2.85,
    },
  ],
  time_series: Array.from({ length: 30 }, (_, i) => ({
    bucket: new Date(Date.now() - (30 - i) * 24 * 3600 * 1000).toISOString().split("T")[0],
    value: Math.floor(400 + Math.random() * 300),
  })),
  token_time_series: Array.from({ length: 30 }, (_, i) => {
    const prompt = Math.floor(1000000 + Math.random() * 500000);
    const completion = Math.floor(300000 + Math.random() * 20000);
    return {
      bucket: new Date(Date.now() - (30 - i) * 24 * 3600 * 1000).toISOString().split("T")[0],
      prompt_tokens: prompt,
      completion_tokens: completion,
      total_tokens: prompt + completion,
    };
  }),
};

const DEMO_TOOLS: ToolMetrics = {
  total_calls: 32050,
  success_count: 31800,
  error_count: 250,
  success_rate: 99.2,
  avg_duration_ms: 220,
  by_tool: [
    {
      tool_name: "search_db",
      call_count: 15000,
      avg_duration_ms: 180,
      error_count: 50,
      success_rate: 99.6,
    },
    {
      tool_name: "fetch_web_page",
      call_count: 12050,
      avg_duration_ms: 450,
      error_count: 180,
      success_rate: 98.5,
    },
    {
      tool_name: "execute_python",
      call_count: 5000,
      avg_duration_ms: 80,
      error_count: 20,
      success_rate: 99.6,
    },
  ],
  time_series: Array.from({ length: 30 }, (_, i) => ({
    bucket: new Date(Date.now() - (30 - i) * 24 * 3600 * 1000).toISOString().split("T")[0],
    value: Math.floor(900 + Math.random() * 400),
  })),
};

const DEMO_ERRORS: ErrorBreakdown = {
  total_errors: 405,
  by_model: [
    { name: "gpt-4o", error_count: 85, span_type: "llm" },
    { name: "claude-3-5-sonnet", error_count: 45, span_type: "llm" },
    { name: "gemini-1.5-pro", error_count: 25, span_type: "llm" },
  ],
  by_tool: [
    { name: "fetch_web_page", error_count: 180, span_type: "tool" },
    { name: "search_db", error_count: 50, span_type: "tool" },
    { name: "execute_python", error_count: 20, span_type: "tool" },
  ],
  time_series: Array.from({ length: 30 }, (_, i) => ({
    bucket: new Date(Date.now() - (30 - i) * 24 * 3600 * 1000).toISOString().split("T")[0],
    value: Math.floor(5 + Math.random() * 15),
  })),
};

const DEMO_SESSIONS: ConversationSession[] = [
  {
    session_id: "sess_01HZF1A1A1",
    tenant_id: "tnt_demo",
    agent_id: "agent_support_bot",
    agent_name: "Customer Support Copilot",
    started_at: new Date(Date.now() - 15 * 60000).toISOString(),
    last_active_at: new Date(Date.now() - 2 * 60000).toISOString(),
    turn_count: 5,
    total_tokens: 15400,
    total_cost_usd: 0.046,
    status: "active",
  },
  {
    session_id: "sess_01HZF1A2B2",
    tenant_id: "tnt_demo",
    agent_id: "agent_coding_assistant",
    agent_name: "Dev Coding Assistant",
    started_at: new Date(Date.now() - 45 * 60000).toISOString(),
    last_active_at: new Date(Date.now() - 10 * 60000).toISOString(),
    turn_count: 12,
    total_tokens: 58000,
    total_cost_usd: 0.174,
    status: "active",
  },
  {
    session_id: "sess_01HZF1A3C3",
    tenant_id: "tnt_demo",
    agent_id: "agent_analyst",
    agent_name: "Data Analyst Agent",
    started_at: new Date(Date.now() - 3 * 3600000).toISOString(),
    last_active_at: new Date(Date.now() - 2.5 * 3600000).toISOString(),
    turn_count: 8,
    total_tokens: 34000,
    total_cost_usd: 0.092,
    status: "completed",
  },
  {
    session_id: "sess_01HZF1A4D4",
    tenant_id: "tnt_demo",
    agent_id: "agent_support_bot",
    agent_name: "Customer Support Copilot",
    started_at: new Date(Date.now() - 5 * 3600000).toISOString(),
    last_active_at: new Date(Date.now() - 4.8 * 3600000).toISOString(),
    turn_count: 4,
    total_tokens: 11200,
    total_cost_usd: 0.033,
    status: "completed",
  },
];

const DEMO_SESSION_DETAIL: SessionDetail = {
  session: DEMO_SESSIONS[0],
  turns: [
    {
      execution_id: "exec_01HZF1B1B1",
      sequence: 1,
      agent_id: "agent_support_bot",
      agent_name: "Customer Support Copilot",
      status: "COMPLETED",
      started_at: new Date(Date.now() - 15 * 60000).toISOString(),
      ended_at: new Date(Date.now() - 14 * 60000 - 45000).toISOString(),
      total_tokens: 3000,
      total_cost_usd: 0.009,
      duration_ms: 15000,
      llm_calls: [
        {
          span_id: "span_llm_1",
          model_name: "claude-3-5-sonnet",
          total_tokens: 3000,
          latency_ms: 2200,
          status: "COMPLETED",
        },
      ],
    },
    {
      execution_id: "exec_01HZF1B2C2",
      sequence: 2,
      agent_id: "agent_support_bot",
      agent_name: "Customer Support Copilot",
      status: "COMPLETED",
      started_at: new Date(Date.now() - 12 * 60000).toISOString(),
      ended_at: new Date(Date.now() - 11 * 60000 - 30000).toISOString(),
      total_tokens: 3100,
      total_cost_usd: 0.009,
      duration_ms: 30000,
      llm_calls: [
        {
          span_id: "span_llm_2",
          model_name: "claude-3-5-sonnet",
          total_tokens: 3100,
          latency_ms: 1800,
          status: "COMPLETED",
        },
      ],
      tool_calls: [
        {
          span_id: "span_tool_1",
          tool_name: "search_db",
          duration_ms: 150,
          status: "COMPLETED",
        },
      ],
    },
    {
      execution_id: "exec_01HZF1B3D3",
      sequence: 3,
      agent_id: "agent_support_bot",
      agent_name: "Customer Support Copilot",
      status: "COMPLETED",
      started_at: new Date(Date.now() - 8 * 60000).toISOString(),
      ended_at: new Date(Date.now() - 7 * 60000 - 40000).toISOString(),
      total_tokens: 3150,
      total_cost_usd: 0.009,
      duration_ms: 20000,
      llm_calls: [
        {
          span_id: "span_llm_3",
          model_name: "claude-3-5-sonnet",
          total_tokens: 3150,
          latency_ms: 2100,
          status: "COMPLETED",
        },
      ],
      tool_calls: [
        {
          span_id: "span_tool_2",
          tool_name: "fetch_web_page",
          duration_ms: 600,
          status: "COMPLETED",
        },
      ],
    },
  ],
};

const DEMO_PROMPTS: PromptRecord[] = [
  {
    id: "pmpt_01HZF1Z1Z1",
    tenant_id: "tnt_demo",
    name: "customer_onboarding_email",
    version: 3,
    template: "Hello {{name}},\n\nWelcome to {{company}}! We're excited to have you on board. To get started, please follow these steps:\n{{steps}}\n\nBest regards,\n{{sender}}",
    variables: ["name", "company", "steps", "sender"],
    model_hints: ["claude-3-5-sonnet", "gpt-4o"],
    metadata: { usage: "marketing", tone: "friendly" },
    created_at: new Date(Date.now() - 5 * 24 * 3600 * 1000).toISOString(),
    usage_count: 1450,
    avg_tokens: 450,
    avg_latency_ms: 620,
  },
  {
    id: "pmpt_01HZF1Z2Y2",
    tenant_id: "tnt_demo",
    name: "code_review_assistant",
    version: 1,
    template: "You are an expert software engineer. Review the following code diff for bugs, safety issues, and performance optimizations. Provide specific constructive suggestions.\n\nCode Diff:\n{{diff}}",
    variables: ["diff"],
    model_hints: ["claude-3-5-sonnet"],
    metadata: { usage: "dev", language: "go" },
    created_at: new Date(Date.now() - 12 * 24 * 3600 * 1000).toISOString(),
    usage_count: 820,
    avg_tokens: 1850,
    avg_latency_ms: 1450,
  },
  {
    id: "pmpt_01HZF1Z3X3",
    tenant_id: "tnt_demo",
    name: "sql_generator",
    version: 2,
    template: "Given the database schema:\n{{schema}}\n\nTranslate the following natural language request into a valid and efficient SQL query:\nRequest: {{request}}",
    variables: ["schema", "request"],
    model_hints: ["gpt-4o"],
    metadata: { usage: "data", db: "postgres" },
    created_at: new Date(Date.now() - 8 * 24 * 3600 * 1000).toISOString(),
    usage_count: 2400,
    avg_tokens: 820,
    avg_latency_ms: 780,
  },
];

// ─── REACT HOOKS ────────────────────────────────────────────

export function useLLMMetrics(from?: string, to?: string) {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<LLMMetrics, Error>(
    demo || !ready || !orgId ? null : `analytics:llm:${orgId}:${from}:${to}`,
    () => client.llmMetrics(from, to),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    return {
      data: DEMO_LLM,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_LLM,
    };
  }
  return swr;
}

export function useToolMetrics(from?: string, to?: string) {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<ToolMetrics, Error>(
    demo || !ready || !orgId ? null : `analytics:tools:${orgId}:${from}:${to}`,
    () => client.toolMetrics(from, to),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    return {
      data: DEMO_TOOLS,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_TOOLS,
    };
  }
  return swr;
}

export function useErrorMetrics(from?: string, to?: string) {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<ErrorBreakdown, Error>(
    demo || !ready || !orgId ? null : `analytics:errors:${orgId}:${from}:${to}`,
    () => client.errorMetrics(from, to),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    return {
      data: DEMO_ERRORS,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_ERRORS,
    };
  }
  return swr;
}

export function useListSessions(limit?: number, offset?: number) {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<ConversationSession[], Error>(
    demo || !ready || !orgId ? null : `analytics:sessions:${orgId}:${limit}:${offset}`,
    () => client.listSessions(limit, offset),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    return {
      data: DEMO_SESSIONS,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_SESSIONS,
    };
  }
  return swr;
}

export function useSessionDetail(id: string) {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<SessionDetail, Error>(
    demo || !ready || !orgId || !id ? null : `analytics:session:${orgId}:${id}`,
    () => client.getSession(id),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    // If id matches any demo session, populate its session header
    const matchedSess = DEMO_SESSIONS.find((s) => s.session_id === id) || DEMO_SESSIONS[0];
    const data = {
      session: matchedSess,
      turns: DEMO_SESSION_DETAIL.turns,
    };
    return {
      data,
      error: undefined,
      isLoading: false,
      mutate: async () => data,
    };
  }
  return swr;
}

export function useListPrompts() {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<PromptRecord[], Error>(
    demo || !ready || !orgId ? null : `prompts:${orgId}`,
    () => client.listPrompts(),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    return {
      data: DEMO_PROMPTS,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_PROMPTS,
    };
  }
  return swr;
}

export function usePromptDetail(id: string) {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();

  const swr = useSWR<PromptRecord, Error>(
    demo || !ready || !orgId || !id ? null : `prompt:${orgId}:${id}`,
    () => client.getPrompt(id),
    { revalidateOnFocus: false, keepPreviousData: true }
  );

  if (demo) {
    const data = DEMO_PROMPTS.find((p) => p.id === id) || DEMO_PROMPTS[0];
    return {
      data,
      error: undefined,
      isLoading: false,
      mutate: async () => data,
    };
  }
  return swr;
}

export function useCreatePrompt() {
  const demo = useDemoMode();
  const { client, orgId } = useMeClient();
  const { trigger, isMutating } = useSWRMutation(
    orgId ? `prompts:${orgId}` : null,
    async (
      _key: string,
      { arg }: { arg: Parameters<typeof client.createPrompt>[0] }
    ) => {
      if (demo) {
        // Return dummy prompt
        const dummy: PromptRecord = {
          id: "pmpt_" + Math.random().toString(36).substring(7),
          tenant_id: "tnt_demo",
          name: arg.name,
          version: 1,
          template: arg.template,
          variables: arg.variables || [],
          model_hints: arg.model_hints || [],
          metadata: arg.metadata || {},
          created_at: new Date().toISOString(),
          usage_count: 0,
          avg_tokens: 0,
          avg_latency_ms: 0,
        };
        return dummy;
      }
      return client.createPrompt(arg);
    }
  );
  return { trigger, isMutating };
}
