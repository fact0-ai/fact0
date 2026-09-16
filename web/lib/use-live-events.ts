"use client";
import { parseCapturedJSON } from "./captured-json";

import { useEffect, useRef, useState } from "react";

import { browserApiURL } from "./api-origin";
import { useAuditClient } from "./audit-api";
import type { AuditEvent } from "./audit-types";
import { authClient } from "./auth-client";
import { useDemoMode } from "./demo/demo-context";
import { DEMO_STREAM_SNAPSHOT } from "./demo/fixtures";

// Maximum consecutive failed reconnect attempts before giving up.
// At the cap (30s backoff) we'd be retrying forever - cap at 8 attempts
// (~2 + 4 + 8 + 16 + 30 + 30 + 30 + 30 ≈ 2.5 min) so the browser
// doesn't hammer the backend indefinitely.
const MAX_RETRIES = 8;

interface State {
  connected: boolean;
  /** True once EventSource fires `open` at least once (lifetime of this hook mount). */
  everOpened: boolean;
  /** Set when MAX_RETRIES consecutive attempts all failed. */
  gaveUp: boolean;
  events: AuditEvent[];
  lastReceivedAt: number | null;
}

interface Opts {
  bufferSize?: number;
  /**
   * Applied BEFORE buffering, so a busy tenant can't crowd the buffer with
   * unrelated events. May be an inline lambda — it's read through a ref and
   * never re-triggers the connection effect.
   */
  filter?: (e: AuditEvent) => boolean;
}

/**
 * useLiveAuditStream opens an authenticated SSE connection to
 * /v1/events/stream and accumulates events into a bounded buffer.
 *
 * Auth: we cannot set headers on EventSource, so we exchange the
 * Better Auth JWT for a short-lived ticket via POST /v1/me/sse-ticket
 * and put THAT in the URL.
 *
 * The JWT is fetched via the shared module-level fetchToken (see
 * use-me.ts), which deduplicates concurrent calls and caches the result
 * for 14 minutes - so reconnects don't fire extra /api/auth/token calls.
 *
 * Retry uses exponential backoff starting at 3s, capped at 30s.
 * After MAX_RETRIES consecutive failures the hook stops retrying and
 * sets `gaveUp: true`.
 *
 * **Important - Next.js dev rewrites**: proxying SSE through `rewrite()`
 * can buffer the response so `EventSource` never reaches READY. If
 * "Connecting…" never clears, set NEXT_PUBLIC_API_URL in `.env.local`
 * so the stream hits the Go backend directly.
 */
export function useLiveAuditStream(opts: Opts = {}) {
  const bufferSize = opts.bufferSize ?? 50;
  const demo = useDemoMode();
  const { data: session, isPending } = authClient.useSession();
  const { data: activeOrg } = authClient.useActiveOrganization();
  const client = useAuditClient();
  const isLoaded = !isPending;
  const isSignedIn = !!session?.user;
  const orgId = activeOrg?.id ?? null;

  const [state, setState] = useState<State>({
    connected: false,
    everOpened: false,
    gaveUp: false,
    events: [],
    lastReceivedAt: null,
  });
  const retryRef = useRef<number>(0);
  const filterRef = useRef<Opts["filter"]>(opts.filter);
  useEffect(() => { filterRef.current = opts.filter; }, [opts.filter]);

  useEffect(() => {
    if (demo) return;

    if (!isLoaded || !isSignedIn || !orgId) return;

    let cancelled = false;
    let source: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    const connect = async (reset: boolean) => {
      if (cancelled) return;

      if (reset) {
        setState({
          connected: false,
          everOpened: false,
          gaveUp: false,
          events: [],
          lastReceivedAt: null,
        });
      }

      try {
        const { ticket } = await client.sseTicket();
        if (cancelled) return;
        const streamPath = `/v1/events/stream?ticket=${encodeURIComponent(ticket)}`;
        source = new EventSource(browserApiURL(streamPath), { withCredentials: false });

        source.addEventListener("open", () => {
          retryRef.current = 0;
          setState((s) => ({ ...s, connected: true, everOpened: true, gaveUp: false }));
        });

        source.addEventListener("event", (e: MessageEvent) => {
          try {
            const evt = parseCapturedJSON(e.data) as AuditEvent;
            if (filterRef.current && !filterRef.current(evt)) return;
            setState((s) => {
              const events = [evt, ...s.events].slice(0, bufferSize);
              return {
                ...s,
                connected: true,
                everOpened: true,
                events,
                lastReceivedAt: Date.now(),
              };
            });
          } catch {
            // malformed frame - drop silently
          }
        });

        source.addEventListener("error", () => {
          setState((s) => ({ ...s, connected: false }));
          source?.close();
          source = null;
          if (cancelled) return;
          scheduleReconnect();
        });
      } catch {
        // ticket mint failed (likely transient: network / session)
        if (cancelled) return;
        scheduleReconnect();
      }
    };

    const scheduleReconnect = () => {
      retryRef.current += 1;
      if (retryRef.current > MAX_RETRIES) {
        setState((s) => ({ ...s, gaveUp: true }));
        return;
      }
      // Start at 3s (not 2s) - token fetch itself takes ~100ms cached /
      // 2-3s cold, so 2s was almost always a no-op wait before the fix.
      const backoff = Math.min(3000 * 2 ** (retryRef.current - 1), 30_000);
      reconnectTimer = setTimeout(() => void connect(false), backoff);
    };

    void connect(true);

    return () => {
      cancelled = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      source?.close();
    };
  }, [client, demo, isLoaded, isSignedIn, orgId, bufferSize]);

  if (demo) {
    return DEMO_STREAM_SNAPSHOT;
  }

  return state;
}
