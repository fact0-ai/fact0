import {
  LLMMetrics,
  ToolMetrics,
  ErrorBreakdown,
  ConversationSession,
  SessionDetail,
  PromptRecord
} from "./analytics-types";

// Typed client for the JWT-authenticated /v1/me/* surface. Every call
// here is fired from the dashboard; the bearer token is a fresh Better
// Auth JWT (issued by the JWT plugin), which the backend verifies
// against its JWKS endpoint and turns into a (user, org → tenant)
// principal.

export interface MeTenant {
  id: string;
  name: string;
  created_at: string;
}

export interface BootstrapResponse {
  tenant: MeTenant;
  key?: string; // raw secret - only present on first provision
  key_id: string;
  scope: "read" | "write";
  provisioned: boolean;
}

export interface MeStatus {
  tenant_id: string;
  has_keys: boolean;
  /**
   * Any signal we'd call "alive" for this tenant. Today that's
   * strictly audit events; future work (phase-2b telemetry scoping)
   * will OR in execution-telemetry counts.
   */
  has_activity: boolean;
  event_count: number;
  chain_height: number;
  /**
   * 0 = onboarding complete, otherwise the *next* step to do.
   *   1 → create an organization (handled by Better Auth; we never return this)
   *   2 → generate an API key
   *   3 → install SDK
   *   4 → send first trace
   */
  onboarding_step: 0 | 1 | 2 | 3 | 4;
}

export interface KeyRow {
  id: string;
  scope: "read" | "write";
  label?: string;
  created_at: string;
  revoked: boolean;
  revoked_at?: string;
}

export interface MePlan {
  id: string;
  name: string;
  retention_days?: number | null;
  monthly_event_limit?: number | null;
  copilot_transcript_retention_days?: number | null;
  description?: string;
}

export interface MePlanResponse {
  plan: MePlan;
  retention_days?: number | null;
  monthly_event_limit?: number | null;
}

export interface MeUsage {
  window_days: number;
  audit_events: number;
  audit_events_this_month?: number;
  monthly_event_limit?: number | null;
  usage_pct?: number | null;
  executions_total: number;
  spans_total: number;
  tenant_scoped: boolean;
}

export interface ShareLink {
  id: string;
  tenant_id: string;
  label: string;
  expires_at: string;
  revoked_at?: string | null;
  access_count: number;
  filter_from?: string | null;
  filter_to?: string | null;
  created_at: string;
  token?: string;
}

export interface AlertSettings {
  alert_webhook_url?: string;
  alert_email?: string;
}

export interface Transaction {
  id: string;
  tenant_id: string;
  stripe_invoice_id?: string;
  amount_cents: number;
  currency: string;
  status: string;
  hosted_invoice_url?: string;
  invoice_pdf?: string;
  created_at: string;
  updated_at: string;
}

export interface InstrumentationResponse {
  actions: string[];
  since: string;
}

class MeAPIError extends Error {
  constructor(public status: number, public body: string) {
    super(`me-api ${status}: ${body}`);
  }
}

type TokenFn = () => Promise<string | null | undefined>;

function unwrap(getToken: TokenFn) {
  return async function fetcher<T>(path: string, init?: RequestInit): Promise<T> {
    const token = await getToken();
    if (!token) throw new MeAPIError(401, "no session token");
    const res = await fetch(path, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
        ...(init?.headers || {}),
      },
    });
    if (!res.ok) {
      throw new MeAPIError(res.status, await res.text().catch(() => ""));
    }
    if (res.status === 204) return undefined as unknown as T;
    return (await res.json()) as T;
  };
}

export function meClient(getToken: TokenFn) {
  const f = unwrap(getToken);
  return {
    bootstrap: () => f<BootstrapResponse>("/v1/me/bootstrap", { method: "POST" }),
    status: () => f<MeStatus>("/v1/me/status"),
    listKeys: () => f<{ keys: KeyRow[] }>("/v1/me/keys"),
    createKey: (scope: "read" | "write", label?: string) =>
      f<{ id: string; key: string; scope: string; label?: string; created_at: string }>(
        "/v1/me/keys",
        { method: "POST", body: JSON.stringify({ scope, label }) }
      ),
    revokeKey: (id: string) => f<void>(`/v1/me/keys/${encodeURIComponent(id)}`, { method: "DELETE" }),
    plan: () => f<MePlanResponse>("/v1/me/plan"),
    usage: () => f<MeUsage>("/v1/me/usage"),
    instrumentation: () => f<InstrumentationResponse>("/v1/me/instrumentation"),
    llmMetrics: (from?: string, to?: string) =>
      f<LLMMetrics>(`/v1/me/analytics/llm?from=${encodeURIComponent(from || "")}&to=${encodeURIComponent(to || "")}`),
    toolMetrics: (from?: string, to?: string) =>
      f<ToolMetrics>(`/v1/me/analytics/tools?from=${encodeURIComponent(from || "")}&to=${encodeURIComponent(to || "")}`),
    errorMetrics: (from?: string, to?: string) =>
      f<ErrorBreakdown>(`/v1/me/analytics/errors?from=${encodeURIComponent(from || "")}&to=${encodeURIComponent(to || "")}`),
    listSessions: (limit?: number, offset?: number) =>
      f<ConversationSession[]>(`/v1/me/analytics/sessions?limit=${limit || 20}&offset=${offset || 0}`),
    getSession: (id: string) =>
      f<SessionDetail>(`/v1/me/analytics/sessions/${encodeURIComponent(id)}`),
    listPrompts: () =>
      f<PromptRecord[]>("/v1/me/prompts"),
    createPrompt: (body: {
      name: string;
      template: string;
      variables?: string[];
      model_hints?: string[];
      metadata?: Record<string, string>;
    }) =>
      f<PromptRecord>("/v1/me/prompts", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    getPrompt: (id: string) =>
      f<PromptRecord>(`/v1/me/prompts/${encodeURIComponent(id)}`),
  };
}

export type MeClient = ReturnType<typeof meClient>;
export { MeAPIError };
