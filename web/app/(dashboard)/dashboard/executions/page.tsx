"use client";

import { AlertCircle, ChevronRight, Clock, RefreshCw, Terminal } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useMemo } from "react";
import useSWR from "swr";

import { PageGetStarted } from "@/components/dashboard/page-get-started";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { RetentionBanner } from "@/components/dashboard/retention-banner";
import { useTelemetryClient } from "@/lib/api";
import { useDashboardHref } from "@/lib/demo/demo-context";
import { docsHref } from "@/lib/docs-origin";
import type { Execution, ExecutionStatus } from "@/lib/types";
import { useTour } from "@/lib/use-tour";
import {
  STATUS_COLORS,
  cn,
  formatDuration,
  relativeTime,
} from "@/lib/utils";

interface ExecListResponse {
  executions: Execution[];
  total: number;
}

const VALID_STATUSES: ExecutionStatus[] = [
  "RUNNING",
  "COMPLETED",
  "FAILED",
  "CANCELLED",
];

export default function ExecutionsPage() {
  return (
    <Suspense fallback={<ExecutionsPageFallback />}>
      <ExecutionsPageContent />
    </Suspense>
  );
}

function ExecutionsPageFallback() {
  return (
    <>
      <PageToolbar title="Executions" showFilters={false} />
      <div className="p-6 lg:p-8">
        <SkeletonList />
      </div>
    </>
  );
}

function ExecutionsPageContent() {
  const searchParams = useSearchParams();
  const { client, ready, orgId } = useTelemetryClient();
  const { startTourFromPage } = useTour();

  const helpContent = {
    title: "Executions",
    tagline: "Every complete agent run, with full span traces, timing breakdowns, and causality graphs.",
    icon: <Terminal className="size-4" />,
    concepts: [
      { title: "Execution", body: "A single end-to-end agent run from trigger to completion, with a status of COMPLETED, FAILED, RUNNING, or CANCELLED." },
      { title: "Spans", body: "Child steps inside an execution - LLM calls, tool invocations, state mutations - each with latency, input/output, and status." },
      { title: "DAG", body: "The parent-child span graph showing which actions caused which. Useful for tracing causality in multi-step agent failures." },
    ],
    docHref: docsHref("concepts/executions"),
    onTourStart: startTourFromPage,
  };

  // Filters seeded from URL - set by the chat sidebar's navigate_to tool
  // ("show failed executions for agent X" → ?agent_id=X&status=FAILED).
  // Plain visits to /dashboard/executions still see the full list.
  const filters = useMemo(() => {
    const agentId = searchParams.get("agent_id") ?? undefined;
    const rawStatus = searchParams.get("status")?.toUpperCase();
    const status =
      rawStatus && (VALID_STATUSES as string[]).includes(rawStatus)
        ? (rawStatus as ExecutionStatus)
        : undefined;
    return { agent_id: agentId, status };
  }, [searchParams]);

  const swrKey =
    ready && orgId
      ? `executions:${orgId}:page_size=50&agent_id=${filters.agent_id ?? ""}&status=${filters.status ?? ""}`
      : null;

  const { data, error, isLoading, mutate } = useSWR<ExecListResponse, Error>(
    swrKey,
    async () => {
      const r = await client.listExecutions({
        page_size: 50,
        agent_id: filters.agent_id,
        status: filters.status,
      });
      return { executions: r.executions || [], total: r.total || 0 };
    },
    { refreshInterval: 5_000, revalidateOnFocus: true },
  );

  const executions = data?.executions ?? [];
  const total = data?.total ?? 0;
  const loading = isLoading;
  const filterActive = !!(filters.agent_id || filters.status);

  return (
    <>
      <PageToolbar title="Executions" showFilters={false} helpContent={helpContent}>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-[11px] text-zinc-500 tabular-nums">
              {total.toLocaleString()} {total === 1 ? "trace" : "traces"} recorded
            </span>
            {filterActive && (
              <span className="text-[10px] font-medium uppercase tracking-wider px-2 py-0.5 rounded-md bg-indigo-500/10 text-indigo-500">
                filtered
                {filters.agent_id ? ` · agent=${filters.agent_id}` : ""}
                {filters.status ? ` · ${filters.status.toLowerCase()}` : ""}
              </span>
            )}
          </div>
          <button
            type="button"
            onClick={() => mutate()}
            disabled={loading}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-medium text-zinc-500 hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50"
          >
            <RefreshCw className={cn("size-3", loading && "animate-spin")} />
            {loading ? "Refreshing…" : "Refresh"}
          </button>
        </div>
      </PageToolbar>

      <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-4">
        <RetentionBanner />
        {error && (
          <ErrorBanner message={error.message} onRetry={() => mutate()} />
        )}

        {!loading && executions.length === 0 && !error && (
          <PageGetStarted
            icon={<Terminal className="size-5 text-zinc-400" />}
            title="No executions yet"
            description="Executions represent a complete agent thread - from first tool call to final output. Wrap your agent with the Fact0 SDK to start capturing traces."
            docLinks={[
              { label: "Quickstart", href: docsHref("quickstart") },
              { label: "Execution concepts", href: docsHref("concepts/executions") },
            ]}
            onTourStart={startTourFromPage}
          />
        )}

        {loading && executions.length === 0 && (
          <SkeletonList />
        )}

        {executions.length > 0 && (
          <ul data-tour="executions-list" className="rounded-xl border border-border/60 bg-card divide-y divide-border/40 overflow-hidden">
            {executions.map((exec) => (
              <ExecutionRow key={exec.id} exec={exec} />
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

function ExecutionRow({ exec }: { exec: Execution }) {
  const executionsBase = useDashboardHref("/executions");
  const durationMs = exec.ended_at
    ? new Date(exec.ended_at).getTime() - new Date(exec.started_at).getTime()
    // eslint-disable-next-line react-hooks/purity
    : Date.now() - new Date(exec.started_at).getTime();
  const statusColor = STATUS_COLORS[exec.status] || "#6b7280";

  return (
    <li>
      <Link
        href={`${executionsBase}/${exec.id}`}
        className="flex items-center gap-4 px-5 py-3 hover:bg-muted/30 transition-colors group"
      >
        <span
          className={cn(
            "size-2 rounded-full shrink-0",
            exec.status === "RUNNING" && "animate-pulse",
          )}
          style={{ backgroundColor: statusColor }}
        />

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3 min-w-0">
            <span className="text-sm font-medium text-foreground dark:text-white truncate">
              {exec.agent_name || exec.agent_id}
            </span>
            <span className="text-[10px] font-mono text-zinc-400 uppercase tracking-wider truncate">
              {exec.id.slice(0, 16)}
            </span>
          </div>
          <div className="flex items-center gap-4 mt-1 text-[11px] text-zinc-500">
            {exec.trigger && (
              <span className="truncate">
                <span className="text-zinc-400">Trigger: </span>
                <span className="font-mono">{exec.trigger}</span>
              </span>
            )}
            <span className="inline-flex items-center gap-1 shrink-0">
              <Clock className="size-3" />
              <span className="font-mono tabular-nums">{formatDuration(durationMs)}</span>
            </span>
            <span className="shrink-0 uppercase tracking-wide tabular-nums">
              {relativeTime(exec.started_at)}
            </span>
          </div>
        </div>

        <span
          className="text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded shrink-0"
          style={{ backgroundColor: `${statusColor}1A`, color: statusColor }}
        >
          {exec.status}
        </span>
        <ChevronRight className="size-3.5 text-zinc-300 dark:text-zinc-700 group-hover:text-indigo-500 group-hover:translate-x-0.5 transition-all shrink-0" />
      </Link>
    </li>
  );
}


function SkeletonList() {
  return (
    <ul className="rounded-xl border border-border/60 bg-card divide-y divide-border/40 overflow-hidden">
      {[0, 1, 2, 3].map((i) => (
        <li key={i} className="px-5 py-3 flex items-center gap-4">
          <div className="size-2 rounded-full bg-muted animate-pulse" />
          <div className="flex-1 space-y-2">
            <div className="h-3 w-40 bg-muted rounded animate-pulse" />
            <div className="h-2.5 w-64 bg-muted/70 rounded animate-pulse" />
          </div>
          <div className="h-4 w-16 bg-muted rounded animate-pulse" />
        </li>
      ))}
    </ul>
  );
}

function ErrorBanner({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-red-500/30 bg-red-500/5 px-4 py-3">
      <AlertCircle className="size-4 text-red-500 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="text-xs font-medium text-red-600 dark:text-red-400">
          Failed to load executions
        </p>
        <p className="text-[11px] text-red-500/70 font-mono truncate">{message}</p>
      </div>
      <button
        type="button"
        onClick={onRetry}
        className="text-[11px] font-medium text-red-600 dark:text-red-400 hover:underline shrink-0"
      >
        Retry
      </button>
    </div>
  );
}
