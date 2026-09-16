import { parseCapturedJSON } from "./captured-json";
// Typed client for the audit log REST surface.
//
// In the dashboard this client carries a fresh Better Auth JWT, which
// the backend DualAuth middleware verifies (via JWKS) and projects to
// a read-scope tenant context. The browser NEVER touches a raw
// `alk_live_*` API key - that was the source of an XSS-exfiltration
// risk we deliberately removed. Write endpoints (POST /v1/events,
// /v1/events/batch) are not reachable from the dashboard at all.
//
// For SSE the same JWT is exchanged for a short-lived single-use
// ticket via POST /v1/me/sse-ticket; the ticket goes into the
// EventSource URL, not the JWT itself.

import { useMemo } from "react";

import type {
  AuditEvent,
  AuditFilter,
  AuditListResponse,
  BrokenEvent,
  ReanchorResult,
  VerifyResult,
} from "./audit-types";
import { createDemoAuditClient } from "./demo/demo-audit-client";
import { useDemoMode } from "./demo/demo-context";
import { fetchToken } from "./use-me";

// Re-export so callers don't need a separate import.
export type { BrokenEvent };

const DEFAULT_BASE = "";

type TokenFn = () => Promise<string | null | undefined>;

class AuditAPIError extends Error {
  constructor(public status: number, public body: string) {
    super(`audit-api ${status}: ${body}`);
  }
}

function toQuery(f: AuditFilter): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) {
    if (v === undefined || v === null || v === "") continue;
    q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

export interface SSETicket {
  ticket: string;
  expires_in: number;
}

export function auditClient(getToken: TokenFn) {
  async function authHeaders(extra?: HeadersInit): Promise<HeadersInit> {
    const token = await getToken();
    if (!token) throw new AuditAPIError(401, "no session token");
    return {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...(extra || {}),
    };
  }

  async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${DEFAULT_BASE}${path}`, {
      ...init,
      headers: await authHeaders(init?.headers),
    });
    if (!res.ok) {
      throw new AuditAPIError(res.status, await res.text().catch(() => ""));
    }
    return parseCapturedJSON(await res.text()) as T;
  }

  return {
    listEvents: (filter: AuditFilter) =>
      fetchJSON<AuditListResponse>(`/v1/events${toQuery(filter)}`),
    getEvent: (id: string) =>
      fetchJSON<AuditEvent>(`/v1/events/${encodeURIComponent(id)}`),
    verifyChain: (from?: string, to?: string) =>
      fetchJSON<VerifyResult>(`/v1/verify${toQuery({ from, to } as AuditFilter)}`),

    /** Full scan - returns every broken event, not just the first. */
    verifyDeep: (from?: string, to?: string) =>
      fetchJSON<VerifyResult>(
        `/v1/verify?scan_all=true${toQuery({ from, to } as AuditFilter).replace(/^\?/, "&")}`,
      ),

    /**
     * Download the audit log as a PDF. We use a Bearer-authenticated
     * fetch instead of `<a href download>` so the JWT never lands in
     * a URL - costs us a brief "Preparing PDF…" state for that.
     */
    downloadPDF: async (from?: string, to?: string): Promise<Blob> => {
      const res = await fetch(
        `${DEFAULT_BASE}/v1/export/pdf${toQuery({ from, to } as AuditFilter)}`,
        { headers: await authHeaders() },
      );
      if (!res.ok) {
        throw new AuditAPIError(res.status, await res.text().catch(() => ""));
      }
      return res.blob();
    },

    downloadEvidencePack: async (from?: string, to?: string): Promise<Blob> => {
      const res = await fetch(
        `${DEFAULT_BASE}/v1/export/evidence-pack${toQuery({ from, to } as AuditFilter)}`,
        { headers: await authHeaders() },
      );
      if (!res.ok) {
        throw new AuditAPIError(res.status, await res.text().catch(() => ""));
      }
      return res.blob();
    },

    /**
     * Recompute hash/prev_hash from the first broken event onward and
     * append an immutable "chain.reanchored" meta-event. Use only when
     * the break is caused by a known software bug, not external tampering.
     */
    reanchorChain: (firstBrokenEventId: string, reason: string) =>
      fetchJSON<ReanchorResult>("/v1/chain/reanchor", {
        method: "POST",
        body: JSON.stringify({ first_broken_event_id: firstBrokenEventId, reason }),
      }),

    /**
     * One-shot sweep: finds the globally earliest break and reanchors
     * everything forward in one operation. No event ID required.
     */
    reanchorAll: (reason: string) =>
      fetchJSON<ReanchorResult>("/v1/chain/reanchor-all", {
        method: "POST",
        body: JSON.stringify({ reason }),
      }),

    /**
     * Mint a single-use SSE ticket bound to the caller's tenant. Used
     * by useLiveAuditStream before each EventSource (re)connect.
     */
    sseTicket: () =>
      fetchJSON<SSETicket>("/v1/me/sse-ticket", { method: "POST" }),
  };
}

export type AuditClient = ReturnType<typeof auditClient>;
export { AuditAPIError };

/** Dashboard hook -returns demo fixtures client in incognito mode. */
export function useAuditClient(): AuditClient {
  const demo = useDemoMode();
  return useMemo(
    () => (demo ? createDemoAuditClient() : auditClient(fetchToken)),
    [demo],
  );
}
