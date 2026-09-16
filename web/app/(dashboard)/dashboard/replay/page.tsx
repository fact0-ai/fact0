"use client";
import {
  AlertCircle,
  Clock,
  GitBranch,
  History,
  Layers,
  Play,
  RefreshCw,
} from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import useSWR from "swr";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { useTelemetryClient } from "@/lib/api";
import { useDashboardHref } from "@/lib/demo/demo-context";
import { docsHref } from "@/lib/docs-origin";
import type { Execution } from "@/lib/types";
import { useTour } from "@/lib/use-tour";
import { STATUS_COLORS, cn, formatDuration, relativeTime } from "@/lib/utils";
interface ExecListResponse {
  executions: Execution[];
  total: number;
}
type FilterStatus = "ALL" | "COMPLETED" | "FAILED" | "RUNNING";
const FILTERS: {
  label: string;
  value: FilterStatus;
}[] = [
  { label: "All", value: "ALL" },
  { label: "Completed", value: "COMPLETED" },
  { label: "Failed", value: "FAILED" },
  { label: "Running", value: "RUNNING" },
];
export default function ReplayPage() {
  const { client, ready, orgId } = useTelemetryClient();
  const { startTourFromPage } = useTour();
  const { data, error, isLoading, mutate } = useSWR<ExecListResponse, Error>(
    ready && orgId ? `executions:${orgId}:replay` : null,
    () =>
      client.listExecutions({ page_size: 100 }).then((r) => ({
        executions: r.executions || [],
        total: r.total || 0,
      })),
    { refreshInterval: 0, revalidateOnFocus: false },
  );
  const [activeFilter, setActiveFilter] = useState<FilterStatus>("ALL");
  const helpContent = {
    title: "Replay Browser",
    tagline:
      "Browse and step through past execution traces deterministically - debug failures without re-running live inference.",
    icon: <Play className="size-4" />,
    concepts: [
      {
        title: "Replay",
        body: "Re-runs a stored execution trace from a specific span sequence, letting you inspect intermediate agent state without triggering real tool calls.",
      },
      {
        title: "Filtering",
        body: "Filter by COMPLETED, FAILED, or RUNNING to focus on specific outcomes. Failed runs are the most useful for debugging.",
      },
    ],
    docHref: docsHref("sdk/python/telemetry"),
    onTourStart: startTourFromPage,
  };
  const allExecutions = data?.executions ?? [];
  const filtered =
    activeFilter === "ALL"
      ? allExecutions
      : allExecutions.filter((e) => e.status === activeFilter);
  const completedCount = allExecutions.filter(
    (e) => e.status === "COMPLETED",
  ).length;
  const failedCount = allExecutions.filter((e) => e.status === "FAILED").length;
  const runningCount = allExecutions.filter(
    (e) => e.status === "RUNNING",
  ).length;
  return (
    <>
      <PageToolbar
        title="Replay Browser"
        showFilters={false}
        helpContent={helpContent}
      >
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-zinc-500 tabular-nums">
            {allExecutions.length} sessions indexed
          </span>
          <button
            type="button"
            onClick={() => mutate()}
            disabled={isLoading}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-medium text-zinc-500 hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50"
          >
            <RefreshCw className={cn("size-3", isLoading && "animate-spin")} />
            {isLoading ? "Syncing…" : "Refresh"}
          </button>
        </div>
      </PageToolbar>

      <div className="relative animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-6">
        <div className={cn("space-y-6 transition-all duration-300", false)}>
          {/* Stats row */}
          <section className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <StatTile
              label="Total Sessions"
              value={allExecutions.length}
              icon={History}
              loading={isLoading}
            />
            <StatTile
              label="Completed"
              value={completedCount}
              icon={Play}
              accent="emerald"
              loading={isLoading}
            />
            <StatTile
              label="Failed"
              value={failedCount}
              icon={AlertCircle}
              accent="red"
              loading={isLoading}
            />
            <StatTile
              label="Running"
              value={runningCount}
              icon={Layers}
              accent="indigo"
              loading={isLoading}
            />
          </section>

          {error && (
            <div className="flex items-center gap-3 rounded-xl border border-red-500/30 bg-red-500/5 px-4 py-3">
              <AlertCircle className="size-4 text-red-500 shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-xs font-medium text-red-600 dark:text-red-400">
                  Failed to load sessions
                </p>
                <p className="text-[11px] text-red-500/70 font-mono truncate">
                  {error.message}
                </p>
              </div>
              <button
                type="button"
                onClick={() => mutate()}
                className="text-[11px] font-medium text-red-600 dark:text-red-400 hover:underline shrink-0"
              >
                Retry
              </button>
            </div>
          )}

          {/* Filter chips */}
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 mr-1">
              Filter:
            </span>
            {FILTERS.map(({ label, value }) => (
              <button
                key={value}
                type="button"
                onClick={() => setActiveFilter(value)}
                className={cn(
                  "px-2.5 py-1 rounded-md text-[11px] font-medium transition-colors",
                  activeFilter === value
                    ? "bg-foreground text-background"
                    : "text-zinc-500 hover:text-foreground hover:bg-muted/60",
                )}
              >
                {label}
              </button>
            ))}
            {activeFilter !== "ALL" && (
              <span className="text-[11px] text-zinc-500 ml-1 tabular-nums">
                {filtered.length} result{filtered.length !== 1 ? "s" : ""}
              </span>
            )}
          </div>

          {!isLoading && filtered.length === 0 && !error && (
            <div className="rounded-xl border border-border/60 bg-card px-6 py-16 flex flex-col items-center text-center gap-3">
              <div className="size-10 rounded-lg bg-muted border border-border flex items-center justify-center">
                <History className="size-5 text-zinc-400" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-medium text-foreground/90">
                  {activeFilter === "ALL"
                    ? "No replay sessions yet"
                    : `No ${activeFilter.toLowerCase()} sessions`}
                </p>
                <p className="text-xs text-zinc-500 max-w-sm">
                  Run an agent through the Fact0 SDK to record your first
                  execution trace.
                </p>
              </div>
              {activeFilter !== "ALL" && (
                <button
                  type="button"
                  onClick={() => setActiveFilter("ALL")}
                  className="mt-2 px-3 py-1 rounded-md text-[11px] font-medium text-zinc-500 hover:text-foreground hover:bg-muted/60 transition-colors"
                >
                  Clear filter
                </button>
              )}
            </div>
          )}

          {filtered.length > 0 && (
            <ul
              data-tour="replay-list"
              className="rounded-xl border border-border/60 bg-card divide-y divide-border/40 overflow-hidden"
            >
              {filtered.map((exec) => (
                <ReplayRow key={exec.id} exec={exec} />
              ))}
            </ul>
          )}
        </div>

        {false}
      </div>
    </>
  );
}
function ReplayRow({ exec }: { exec: Execution }) {
  const executionsBase = useDashboardHref("/executions");
  const durationMs = exec.ended_at
    ? new Date(exec.ended_at).getTime() - new Date(exec.started_at).getTime()
    : // eslint-disable-next-line react-hooks/purity
      Date.now() - new Date(exec.started_at).getTime();
  const statusColor = STATUS_COLORS[exec.status] || "#6b7280";
  const isCompleted = exec.status === "COMPLETED";
  return (
    <li className="flex items-center gap-4 px-5 py-3 hover:bg-muted/30 transition-colors">
      <span
        className="size-2 rounded-full shrink-0"
        style={{ backgroundColor: statusColor }}
      />

      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-3 min-w-0">
          <span className="text-sm font-medium text-foreground dark:text-white truncate">
            {exec.agent_name || exec.agent_id}
          </span>
          <span
            className="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded shrink-0"
            style={{ backgroundColor: `${statusColor}1A`, color: statusColor }}
          >
            {exec.status}
          </span>
        </div>
        <div className="flex items-center gap-4 mt-1 text-[11px] text-zinc-500 flex-wrap">
          {exec.trigger && (
            <span className="truncate">
              <span className="text-zinc-400">Trigger: </span>
              <span className="font-mono">{exec.trigger}</span>
            </span>
          )}
          <span className="inline-flex items-center gap-1">
            <Clock className="size-3" />
            <span className="font-mono tabular-nums">
              {formatDuration(durationMs)}
            </span>
          </span>
          <span className="inline-flex items-center gap-1">
            <GitBranch className="size-3" />
            <span className="font-mono">{exec.id.slice(0, 16)}</span>
          </span>
          <span className="uppercase tracking-wide tabular-nums">
            {relativeTime(exec.started_at)}
          </span>
        </div>
      </div>

      <Link
        href={`${executionsBase}/${exec.id}`}
        className={cn(
          "shrink-0 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-[11px] font-semibold uppercase tracking-wider transition-colors",
          isCompleted
            ? "bg-indigo-600 text-white hover:bg-indigo-500"
            : "text-zinc-500 hover:text-foreground hover:bg-muted/60",
        )}
      >
        <Play className="size-3" />
        Open Replay
      </Link>
    </li>
  );
}
function StatTile({
  label,
  value,
  icon: Icon,
  accent = "zinc",
  loading,
}: {
  label: string;
  value: number;
  icon: typeof History;
  accent?: "zinc" | "emerald" | "red" | "indigo";
  loading?: boolean;
}) {
  const accentClass = {
    zinc: "text-zinc-400",
    emerald: "text-emerald-500",
    red: "text-red-500",
    indigo: "text-indigo-500",
  }[accent];
  return (
    <div className="rounded-xl border border-border/60 bg-card p-4 space-y-2">
      <div className="flex items-center gap-2 text-zinc-500">
        <Icon className={cn("size-3.5", accentClass)} />
        <span className="text-[10px] font-semibold uppercase tracking-wider">
          {label}
        </span>
      </div>
      <div className="text-2xl font-semibold text-foreground dark:text-white tabular-nums">
        {loading ? "-" : value.toLocaleString()}
      </div>
    </div>
  );
}
