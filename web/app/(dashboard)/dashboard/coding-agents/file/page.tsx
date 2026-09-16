"use client";

import {
  AlertCircle,
  ArrowLeft,
  Bot,
  ChevronRight,
  FileCode2,
} from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useMemo, useState } from "react";
import useSWR from "swr";

import { useTelemetryClient } from "@/lib/api";
import { useAuditClient } from "@/lib/audit-api";
import type { AuditEvent } from "@/lib/audit-types";
import { useDashboardHref } from "@/lib/demo/demo-context";
import { cn, relativeTime } from "@/lib/utils";

// File-centric history: every coding-agent touch of one file, grouped by
// session — the incident-forensics view ("which AI session edited this?").

export default function FileHistoryPage() {
  return (
    <Suspense
      fallback={
        <div className="p-6 lg:p-8 text-xs text-zinc-500">Loading…</div>
      }
    >
      <FileHistoryContent />
    </Suspense>
  );
}

interface SessionTouches {
  sessionId: string;
  events: AuditEvent[];
  first: string;
  last: string;
  actors: string[];
}

function FileHistoryContent() {
  const params = useSearchParams();
  const path = params.get("path") ?? "";
  const listHref = useDashboardHref("/coding-agents");
  const auditClient = useAuditClient();
  const { ready, orgId } = useTelemetryClient();
  const [page, setPage] = useState(1);

  const { data, error, isLoading } = useSWR(
    ready && path ? ["cc-file-history", orgId, path, page] : null,
    async () => {
      const r = await auditClient.listEvents({
        resource_id: path,
        action: "claude_code.tool.*",
        page,
        page_size: 100,
      });
      return r;
    },
    { refreshInterval: 15_000 },
  );

  const sessions = useMemo<SessionTouches[]>(() => {
    const byS = new Map<string, AuditEvent[]>();
    for (const e of data?.events ?? []) {
      const sid =
        typeof e.metadata?.["session_id"] === "string"
          ? (e.metadata["session_id"] as string)
          : "";
      if (!sid) continue;
      const list = byS.get(sid) ?? [];
      list.push(e);
      byS.set(sid, list);
    }
    return [...byS.entries()]
      .map(([sessionId, events]) => {
        const ts = events.map((e) => e.timestamp).sort();
        return {
          sessionId,
          events,
          first: ts[0],
          last: ts[ts.length - 1],
          actors: [
            ...new Set(events.map((e) => e.actor?.id).filter(Boolean)),
          ] as string[],
        };
      })
      .sort((a, b) => b.last.localeCompare(a.last));
  }, [data]);

  const fileName = path.split("/").filter(Boolean).pop() ?? path;

  return (
    <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-4">
      <Link
        href={listHref}
        className="inline-flex items-center gap-1.5 text-[11px] font-medium text-zinc-500 hover:text-foreground transition-colors"
      >
        <ArrowLeft className="size-3" />
        All coding sessions
      </Link>

      <div className="rounded-xl border border-border/60 bg-card p-5">
        <div className="flex items-center gap-3">
          <FileCode2 className="size-4 text-indigo-500 shrink-0" />
          <h1 className="text-base font-semibold text-foreground dark:text-white truncate">
            {fileName}
          </h1>
        </div>
        <p className="mt-1 text-[11px] font-mono text-zinc-400 truncate">
          {path || "no file selected"}
        </p>
        <p className="mt-2 text-[11px] text-zinc-500">
          {sessions.length}{" "}
          {sessions.length === 1
            ? "session on this page has"
            : "sessions on this page have"}{" "}
          touched this file · {data?.total ?? 0} recorded file events
        </p>
      </div>

      {error && (
        <div className="flex items-center gap-3 rounded-xl border border-red-500/30 bg-red-500/5 px-4 py-3">
          <AlertCircle className="size-4 text-red-500 shrink-0" />
          <p className="text-xs font-medium text-red-600 dark:text-red-400">
            {error.message}
          </p>
        </div>
      )}

      {!isLoading && sessions.length === 0 && !error && (
        <div className="rounded-xl border border-border/60 bg-card p-8 text-center text-xs text-zinc-500">
          No coding-agent activity recorded for this file
          {path ? "" : " — open this page from a session's file row"}.
        </div>
      )}

      {sessions.length > 0 && (
        <ul className="rounded-xl border border-border/60 bg-card divide-y divide-border/40 overflow-hidden">
          {sessions.map((s) => (
            <li key={s.sessionId}>
              <Link
                href={`${listHref}/${encodeURIComponent(s.sessionId)}`}
                className="flex items-center gap-4 px-5 py-3 hover:bg-muted/30 transition-colors group"
              >
                <Bot className="size-3.5 text-indigo-500 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3">
                    <span className="text-xs font-mono text-foreground dark:text-white truncate">
                      {s.sessionId.slice(0, 18)}…
                    </span>
                    <span className="text-[11px] text-zinc-500 shrink-0">
                      {s.events.length}{" "}
                      {s.events.length === 1 ? "edit" : "edits"}
                    </span>
                  </div>
                  <div className="mt-0.5 flex items-center gap-3 text-[11px] text-zinc-500">
                    <span className="uppercase tracking-wide tabular-nums">
                      {relativeTime(s.last)}
                    </span>
                    {s.actors.length > 0 && (
                      <span>by {s.actors.join(", ")}</span>
                    )}
                    <span className={cn("font-mono", "text-zinc-400")}>
                      {s.events
                        .map((e) => e.action.replace("claude_code.tool.", ""))
                        .filter((v, i, a) => a.indexOf(v) === i)
                        .join(" · ")}
                    </span>
                  </div>
                </div>
                <ChevronRight className="size-3.5 text-zinc-300 dark:text-zinc-700 group-hover:text-indigo-500 group-hover:translate-x-0.5 transition-all shrink-0" />
              </Link>
            </li>
          ))}
        </ul>
      )}
      <div className="flex items-center gap-3 text-sm">
        <button
          className="rounded border px-3 py-1 disabled:opacity-40"
          disabled={page === 1}
          onClick={() => setPage((p) => p - 1)}
        >
          Previous
        </button>
        <span>Page {page}</span>
        <button
          className="rounded border px-3 py-1 disabled:opacity-40"
          disabled={page * 100 >= (data?.total ?? 0)}
          onClick={() => setPage((p) => p + 1)}
        >
          Next
        </button>
      </div>
    </div>
  );
}
