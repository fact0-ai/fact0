import { useMemo } from "react";

import { authClient } from "./auth-client";
import { useDemoMode } from "./demo/demo-context";
import { createDemoTelemetryClient } from "./demo/demo-telemetry-client";
import type {
  DAGEdge,
  DAGNode,
  Execution,
  ExecutionEvent,
  ReplayResponse,
  Span,
} from "./types";
import { fetchToken } from "./use-me";

const API_BASE = "";

type TokenFn = () => Promise<string | null | undefined>;

class TelemetryAPIError extends Error {
  constructor(public status: number, public body: string) {
    super(`telemetry-api ${status}: ${body}`);
  }
}

export function telemetryClient(getToken: TokenFn) {
  async function authHeaders(extra?: HeadersInit): Promise<HeadersInit> {
    const token = await getToken();
    if (!token) throw new TelemetryAPIError(401, "no session token");
    return {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...(extra || {}),
    };
  }

  async function fetchJSON<T>(path: string, options?: RequestInit): Promise<T> {
    const res = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers: await authHeaders(options?.headers as HeadersInit | undefined),
    });

    if (!res.ok) {
      const body = await res.text();
      throw new TelemetryAPIError(res.status, body);
    }

    return res.json();
  }

  return {
    listExecutions(params?: {
      page_size?: number;
      offset?: number;
      agent_id?: string;
      status?: string;
    }): Promise<{ executions: Execution[]; total: number }> {
      const query = new URLSearchParams();
      if (params?.page_size) query.set("page_size", String(params.page_size));
      if (params?.offset) query.set("offset", String(params.offset));
      if (params?.agent_id) query.set("agent_id", params.agent_id);
      if (params?.status) query.set("status", params.status);
      const qs = query.toString();
      return fetchJSON(`/api/v1/executions${qs ? `?${qs}` : ""}`);
    },

    getExecution(id: string): Promise<Execution> {
      return fetchJSON(`/api/v1/executions/${id}`);
    },

    getSpans(executionId: string): Promise<{ spans: Span[] }> {
      return fetchJSON(`/api/v1/executions/${executionId}/spans`);
    },

    getSpan(spanId: string): Promise<{ span: Span; events: ExecutionEvent[] }> {
      return fetchJSON(`/api/v1/spans/${spanId}`);
    },

    getSpanEvents(spanId: string): Promise<{ events: ExecutionEvent[] }> {
      return fetchJSON(`/api/v1/spans/${spanId}/events`);
    },

    getExecutionDAG(
      executionId: string,
    ): Promise<{ nodes: DAGNode[]; edges: DAGEdge[] }> {
      return fetchJSON(`/api/v1/executions/${executionId}/dag`);
    },

    replayExecution(
      executionId: string,
      params?: { from_sequence?: number; to_sequence?: number },
    ): Promise<ReplayResponse> {
      const query = new URLSearchParams();
      if (params?.from_sequence) {
        query.set("from_sequence", String(params.from_sequence));
      }
      if (params?.to_sequence) {
        query.set("to_sequence", String(params.to_sequence));
      }
      const qs = query.toString();
      return fetchJSON(
        `/api/v1/executions/${executionId}/replay${qs ? `?${qs}` : ""}`,
      );
    },
  };
}

/** Hook helper - gate SWR on active org and attach JWT to telemetry reads. */
export function useTelemetryClient() {
  const demo = useDemoMode();
  const { data: session, isPending: sessionPending } = authClient.useSession();
  const { data: activeOrg, isPending: activeOrgPending } =
    authClient.useActiveOrganization();

  const client = useMemo(
    () => (demo ? createDemoTelemetryClient() : telemetryClient(fetchToken)),
    [demo],
  );
  const authReady = !sessionPending && !!session?.user;
  const orgHydrated = !activeOrgPending;
  const orgId = activeOrg?.id ?? null;
  const ready = demo || (authReady && orgHydrated && orgId !== null && orgId.length > 0);

  return { client, ready, orgId: demo ? "demo-org" : orgId };
}

// Legacy exports - prefer useTelemetryClient() in dashboard code.
const defaultClient = telemetryClient(fetchToken);

export const listExecutions = defaultClient.listExecutions.bind(defaultClient);
export const getExecution = defaultClient.getExecution.bind(defaultClient);
export const getSpans = defaultClient.getSpans.bind(defaultClient);
export const getSpan = defaultClient.getSpan.bind(defaultClient);
export const getSpanEvents = defaultClient.getSpanEvents.bind(defaultClient);
export const getExecutionDAG = defaultClient.getExecutionDAG.bind(defaultClient);
export const replayExecution = defaultClient.replayExecution.bind(defaultClient);
