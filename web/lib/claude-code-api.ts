// Typed client for the Claude Code sessions aggregation surface
// (GET /api/v1/integrations/claude-code/sessions), which backs the
// dashboard "Coding Agents" view. Same Bearer-JWT auth model as
// lib/audit-api.ts: the backend DualAuth middleware verifies the JWT
// and projects it to a read-scope tenant context.

import { useMemo } from "react";

import { useDemoMode } from "./demo/demo-context";
import { fetchToken } from "./use-me";

const DEFAULT_BASE = "";

type TokenFn = () => Promise<string | null | undefined>;

export interface ClaudeCodeSession {
  session_id: string;
  cwd: string;
  source: string;
  permission_mode: string;
  actor_id: string;
  started_at: string;
  ended_at?: string;
  duration_s: number;
  status: "active" | "idle" | "completed";
  prompts: number;
  tool_calls: number;
  files_touched: number;
  commands: number;
  failures: number;
  git_branch?: string;
  denied: number;
  cost_usd: number;
  tokens: number;
  execution_id?: string;
  last_activity_at: string;
}

export interface ClaudeCodeSessionFilter {
  from?: string;
  to?: string;
  cwd?: string;
  page?: number;
  page_size?: number;
}

export interface ClaudeCodeSessionListResponse {
  sessions: ClaudeCodeSession[];
  total: number;
  page: number;
  page_size: number;
}

class ClaudeCodeAPIError extends Error {
  constructor(public status: number, public body: string) {
    super(`claude-code-api ${status}: ${body}`);
  }
}

function toQuery(f: ClaudeCodeSessionFilter): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) {
    if (v === undefined || v === null || v === "") continue;
    q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

export function claudeCodeClient(getToken: TokenFn) {
  async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const token = await getToken();
    if (!token) throw new ClaudeCodeAPIError(401, "no session token");
    const res = await fetch(`${DEFAULT_BASE}${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
        ...(init?.headers || {}),
      },
    });
    if (!res.ok) {
      throw new ClaudeCodeAPIError(res.status, await res.text().catch(() => ""));
    }
    return (await res.json()) as T;
  }

  return {
    listSessions: (filter: ClaudeCodeSessionFilter = {}) =>
      fetchJSON<ClaudeCodeSessionListResponse>(
        `/api/v1/integrations/claude-code/sessions${toQuery(filter)}`,
      ),
    getSession: (sessionId: string) =>
      fetchJSON<ClaudeCodeSession>(
        `/api/v1/integrations/claude-code/sessions/${encodeURIComponent(sessionId)}`,
      ),

  };
}

export type ClaudeCodeClient = ReturnType<typeof claudeCodeClient>;
export { ClaudeCodeAPIError };

/** Demo/incognito client backed by the narrative session fixtures. */
function demoClaudeCodeClient(): ClaudeCodeClient {
  return {
    listSessions: async (filter = {}) => {
      const { listDemoClaudeCodeSessions } = await import("./demo/demo-claude-code");
      return listDemoClaudeCodeSessions(filter);
    },
    getSession: async (id: string) => {
      const { getDemoClaudeCodeSession } = await import("./demo/demo-claude-code");
      const s = getDemoClaudeCodeSession(id);
      if (!s) throw new ClaudeCodeAPIError(404, `claude_code_session ${id} not found`);
      return s;
    },
  };
}

/** Dashboard hook — returns an empty-data client in demo mode. */
export function useClaudeCodeClient(): ClaudeCodeClient {
  const demo = useDemoMode();
  return useMemo(
    () => (demo ? demoClaudeCodeClient() : claudeCodeClient(fetchToken)),
    [demo],
  );
}
