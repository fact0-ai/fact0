"use client";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import Link from "next/link";
import useSWR from "swr";
import { useClaudeCodeClient } from "@/lib/claude-code-api";
import { useAuditClient } from "@/lib/audit-api";
import { useTelemetryClient } from "@/lib/api";
import { useLiveAuditStream } from "@/lib/use-live-events";
import { CapturedPayload } from "@/components/inspector/captured-payload";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
export default function CodingSessionPage() {
  const { sessionId } = useParams<{ sessionId: string }>();
  const cc = useClaudeCodeClient();
  const audit = useAuditClient();
  const { ready, orgId } = useTelemetryClient();
  const [page, setPage] = useState(1);
  const session = useSWR(
    ready ? ["coding-session", orgId, sessionId] : null,
    () => cc.getSession(sessionId),
    { refreshInterval: 10000 },
  );
  const events = useSWR(
    ready ? ["coding-events", orgId, sessionId, page] : null,
    () =>
      audit.listEvents({
        session_id: sessionId,
        action: "claude_code.*",
        page,
        page_size: 100,
      }),
    { refreshInterval: 10000 },
  );
  const mutateEvents = events.mutate;
  const mutateSession = session.mutate;
  const live = useLiveAuditStream({
    filter: (e) =>
      e.metadata?.session_id === sessionId || e.resource.id === sessionId,
  });
  useEffect(() => {
    if (live.lastReceivedAt) {
      void mutateEvents();
      void mutateSession();
    }
  }, [live.lastReceivedAt, mutateEvents, mutateSession]);
  return (
    <>
      <PageToolbar title="Coding session" showFilters={false} />
      <div className="p-6 lg:p-8 space-y-5">
        <Link
          className="text-sm text-muted-foreground hover:underline"
          href="/dashboard/coding-agents"
        >
          ← All coding sessions
        </Link>
        <div className="rounded-xl border bg-card p-5 space-y-2">
          <h1 className="text-lg font-semibold break-all">
            {session.data?.cwd || sessionId}
          </h1>
          <p className="text-xs text-muted-foreground font-mono">{sessionId}</p>
          <p className="text-sm">
            {session.data?.status ?? "Loading…"} ·{" "}
            {session.data?.tool_calls ?? 0} tool calls ·{" "}
            {session.data?.files_touched ?? 0} files ·{" "}
            {session.data?.tokens ?? 0} tokens
          </p>
        </div>
        <p className="text-sm text-muted-foreground">
          Captured events, newest first. Raw mode can include prompts, file
          contents, shell commands and tool outputs. A complete transcript
          capture does not guarantee that every agent action reached this
          installation.
        </p>
        {(session.error || events.error) && (
          <p role="alert" className="text-sm text-red-600">
            {session.error?.message || events.error?.message}
          </p>
        )}
        {events.isLoading && <p className="text-sm">Loading events…</p>}
        {!events.isLoading && !events.error && !events.data?.events?.length && (
          <p className="rounded-lg border p-5 text-sm">
            No captured events are available for this session.
          </p>
        )}
        <div className="space-y-3">
          {events.data?.events?.map((e) => {
            const md = e.metadata ?? {};
            const mode = String(md.capture_mode ?? "legacy / unknown");
            const status = String(
              md.capture_status ??
                (mode === "raw" ? "recorded" : "limited capture"),
            );
            const reason =
              typeof md.capture_reason === "string"
                ? md.capture_reason
                : undefined;
            return (
              <article key={e.id} className="rounded-xl border bg-card p-4">
                <div className="flex items-center gap-3 flex-wrap">
                  <h2 className="text-sm font-medium font-mono">
                    {e.action.replace("claude_code.", "")}
                  </h2>
                  <span className="rounded bg-muted px-2 py-0.5 text-xs">
                    {mode}
                  </span>
                  <span className="text-xs">{status}</span>
                  <time className="ml-auto text-xs text-muted-foreground">
                    {new Date(e.timestamp).toLocaleString()}
                  </time>
                </div>
                {reason && (
                  <p className="mt-2 text-xs text-amber-700">{reason}</p>
                )}
                {e.resource?.name && (
                  <p className="mt-2 text-xs font-mono break-all">
                    {e.resource.name}
                  </p>
                )}
                {e.resource?.type === "file" && e.resource?.id && (
                  <Link
                    className="mt-2 block text-xs underline"
                    href={
                      "/dashboard/coding-agents/file?path=" +
                      encodeURIComponent(e.resource.id)
                    }
                  >
                    History for this file
                  </Link>
                )}
                {md.prompt !== undefined && (
                  <CapturedPayload label="Prompt" value={md.prompt} />
                )}
                {e.action.startsWith("claude_code.tool.") && (
                  <>
                    <CapturedPayload
                      label="Tool input"
                      value={md.tool_input}
                      status={status}
                    />
                    <CapturedPayload
                      label="Tool output"
                      value={md.tool_response}
                      status={status}
                    />
                  </>
                )}
                {(e.action === "claude_code.turn.complete" ||
                  e.action === "claude_code.subagent.stop") && (
                  <>
                    <CapturedPayload
                      label="Assistant text"
                      value={md.response}
                      status={status}
                      reason={reason}
                    />
                    <CapturedPayload
                      label="Assistant message blocks"
                      value={md.messages}
                    />
                  </>
                )}
                {md.error !== undefined && (
                  <CapturedPayload label="Error" value={md.error} />
                )}
                <CapturedPayload label="Full event metadata" value={md} />
              </article>
            );
          })}
        </div>
        <div className="flex items-center gap-3 text-sm">
          <button
            className="rounded border px-3 py-1 disabled:opacity-40"
            disabled={page === 1}
            onClick={() => setPage((p) => p - 1)}
          >
            Previous
          </button>
          <span>
            Page {page} · {events.data?.total ?? 0} events
          </span>
          <button
            className="rounded border px-3 py-1 disabled:opacity-40"
            disabled={page * 100 >= (events.data?.total ?? 0)}
            onClick={() => setPage((p) => p + 1)}
          >
            Next
          </button>
        </div>
      </div>
    </>
  );
}
