"use client";

import { Activity, ChevronRight } from "lucide-react";
import Link from "next/link";
import { useEffect } from "react";
import useSWR from "swr";

import type { TimeRange } from "@/components/dashboard/page-toolbar";
import { useTelemetryClient } from "@/lib/api";
import { useDashboardHref } from "@/lib/demo/demo-context";
import type { Execution } from "@/lib/types";
import { cn, relativeTime } from "@/lib/utils";

interface Props {
  live?: boolean;
  timeRange?: TimeRange;
  agentId?: string;
  status?: string;
  refreshKey?: number;
}

function sinceForRange(range: TimeRange): Date | null {
  const now = Date.now();
  switch (range) {
    case "1h":
      return new Date(now - 60 * 60 * 1000);
    case "24h":
      return new Date(now - 24 * 60 * 60 * 1000);
    case "7d":
      return new Date(now - 7 * 24 * 60 * 60 * 1000);
    default:
      return null;
  }
}

function filterExecutions(
  rows: Execution[],
  timeRange: TimeRange,
  agentId?: string,
  status?: string,
): Execution[] {
  const since = sinceForRange(timeRange);
  return rows.filter((exec) => {
    if (since && new Date(exec.started_at) < since) return false;
    if (agentId && !exec.agent_id.toLowerCase().includes(agentId.toLowerCase())) return false;
    if (status && exec.status !== status) return false;
    return true;
  });
}

/**
 * Live execution feed - flat panel matching the Agnost reference.
 */
export function LiveFeed({
  live = false,
  timeRange = "1h",
  agentId = "",
  status = "",
  refreshKey = 0,
}: Props) {
  const { client, ready, orgId } = useTelemetryClient();
  const executionsBase = useDashboardHref("/executions");

  const { data, isLoading, mutate } = useSWR<Execution[]>(
    ready && orgId ? `executions:${orgId}:live-feed` : null,
    async () => (await client.listExecutions({ page_size: 50 })).executions,
    {
      // SSE drives real-time updates; this poll is a safety-net fallback.
      refreshInterval: 30_000,
      revalidateOnFocus: false,
      keepPreviousData: true,
    },
  );

  useEffect(() => {
    if (refreshKey > 0) {
      void mutate();
    }
  }, [refreshKey, mutate]);

  const filtered = filterExecutions(data ?? [], timeRange, agentId, status).slice(0, 10);

  return (
    <section data-tour="live-feed" className="rounded-xl border border-border/40 bg-white dark:bg-zinc-900 shadow-sm overflow-hidden">
      <header className="flex items-center justify-between px-5 py-3 border-b border-border/50">
        <div className="flex items-center gap-2 min-w-0">
          <Activity className="size-3.5 text-zinc-400 shrink-0" />
          <h3 className="text-sm font-semibold text-foreground dark:text-white truncate">
            Recent Activity
          </h3>
          <span
            className={cn(
              "ml-2 inline-flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider shrink-0",
              live ? "text-emerald-600 dark:text-emerald-400" : "text-zinc-500",
            )}
          >
            <span
              className={cn(
                "size-1.5 rounded-full",
                live ? "bg-emerald-500 animate-pulse" : "bg-zinc-400",
              )}
            />
            {live ? "Live" : "Polling"}
          </span>
        </div>
        <Link
          href={executionsBase}
          className="text-xs font-medium text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 transition-colors flex items-center gap-0.5 shrink-0"
        >
          View all <ChevronRight className="size-3" />
        </Link>
      </header>

      {filtered.length === 0 && !isLoading ? (
        <EmptyState hasFilters={!!agentId || !!status || timeRange !== "all"} />
      ) : (
        <ul className="divide-y divide-border/40">
          {filtered.map((exec, i) => {
            const fresh = i === 0 && live;
            return (
              <li key={exec.id}>
                <Link
                  href={`${executionsBase}/${exec.id}`}
                  className={cn(
                    "grid grid-cols-[1fr_auto_auto_auto] gap-4 items-center px-5 py-3 text-xs hover:bg-zinc-50 dark:hover:bg-zinc-800/50 transition-colors group",
                    fresh && "bg-zinc-50 dark:bg-zinc-800",
                  )}
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    <span
                      className={cn("size-1.5 rounded-full shrink-0 bg-zinc-300 dark:bg-zinc-700", fresh && "animate-pulse")}
                    />
                    <span className="font-mono text-foreground/90 truncate">
                      {exec.id.slice(0, 24)}
                    </span>
                  </div>
                  <span className="text-zinc-500 truncate max-w-[14ch]">
                    {exec.agent_name || exec.agent_id}
                  </span>
                  <span
                    className="text-[10px] font-medium uppercase tracking-wider px-2 py-0.5 rounded border border-border/50 text-zinc-600 dark:text-zinc-400"
                  >
                    {exec.status}
                  </span>
                  <span className="text-[11px] text-zinc-500 tabular-nums uppercase tracking-wide">
                    {relativeTime(exec.started_at)}
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      )}

      {isLoading && filtered.length === 0 && (
        <div className="px-5 py-3 space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-4 w-full rounded bg-muted/60 animate-pulse" />
          ))}
        </div>
      )}
    </section>
  );
}

function EmptyState({ hasFilters }: { hasFilters: boolean }) {
  return (
    <div className="px-5 py-14 flex flex-col items-center justify-center text-center gap-1">
      <p className="text-sm text-zinc-500">
        {hasFilters ? "No activity matches your filters" : "No recent activity"}
      </p>
      <p className="text-xs text-zinc-400">
        {hasFilters
          ? "Try a wider time range or clear filters."
          : "Events will appear here as your agents emit them."}
      </p>
    </div>
  );
}
