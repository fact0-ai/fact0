"use client";

import {
  Activity,
  BarChart3,
  Database,
  ShieldCheck,
  Wifi,
  Zap,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";

import { DashboardSkeleton } from "@/components/dashboard/dashboard-skeleton";
import { LiveFeed } from "@/components/dashboard/live-feed";
import { MetricCard } from "@/components/dashboard/metric-card";
import { PageToolbar, type TimeRange } from "@/components/dashboard/page-toolbar";
import { OnboardingChecklist } from "@/components/onboarding/checklist";

import type { ExecutionStatus } from "@/lib/types";
import { useLiveAuditStream } from "@/lib/use-live-events";
import { useMeStatus } from "@/lib/use-me";
import { useTour } from "@/lib/use-tour";
import { cn } from "@/lib/utils";

const SSE_STALL_MS = 10_000;

const EXEC_STATUSES: ExecutionStatus[] = ["RUNNING", "COMPLETED", "FAILED", "CANCELLED"];

export default function DashboardPage() {
  const status = useMeStatus();
  const live = useLiveAuditStream();
  const [streamStalled, setStreamStalled] = useState(false);
  const [timeRange, setTimeRange] = useState<TimeRange>("1h");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [agentFilter, setAgentFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState<ExecutionStatus | "">("");
  const [refreshKey, setRefreshKey] = useState(0);

  const { startTour } = useTour();
  const mutateRef = useRef(status.mutate);
  useEffect(() => {
    mutateRef.current = status.mutate;
  }, [status.mutate]);

  const handleRefresh = useCallback(() => {
    void mutateRef.current();
    setRefreshKey((k) => k + 1);
  }, []);

  useEffect(() => {
    if (live.connected || live.everOpened) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setStreamStalled(false);
      return;
    }
    const t = setTimeout(() => setStreamStalled(true), SSE_STALL_MS);
    return () => clearTimeout(t);
  }, [live.connected, live.everOpened]);

  const loading = status.isLoading;

  if (loading) {
    return (
      <>
        <PageToolbar title="Dashboard" showFilters={false}>
          <div className="h-5 w-28 rounded-md bg-muted animate-pulse" aria-hidden />
        </PageToolbar>
        <DashboardSkeleton />
      </>
    );
  }

  if (status.error) {
    return (
      <>
        <PageToolbar title="Dashboard" showFilters={false} />
        <div className="p-8 lg:p-10 max-w-lg mx-auto">
          <div className="rounded-xl border border-red-500/25 bg-red-500/5 px-5 py-4 space-y-3">
            <p className="text-sm font-medium text-red-600 dark:text-red-400">
              Could not load dashboard status
            </p>
            <p className="text-xs text-red-600/80 font-mono break-all">
              {status.error.message}
            </p>
            <button
              type="button"
              onClick={() => void status.mutate()}
              className="text-xs font-medium text-red-700 dark:text-red-300 underline-offset-4 hover:underline"
            >
              Try again
            </button>
          </div>
        </div>
      </>
    );
  }

  if (status.data === undefined) {
    return (
      <>
        <PageToolbar title="Dashboard" showFilters={false}>
          <div className="h-5 w-28 rounded-md bg-muted animate-pulse" aria-hidden />
        </PageToolbar>
        <DashboardSkeleton />
      </>
    );
  }

  if (!status.data.has_activity) {
    return (
      <>
        <PageToolbar title="Get started" showFilters={false} />
        <div className="animate-in fade-in slide-in-from-bottom-4 duration-500 ease-out p-6 lg:p-8 max-w-4xl mx-auto">
          <OnboardingChecklist />
        </div>
      </>
    );
  }

  const chainHeight = status.data?.chain_height ?? 0;
  const eventCount = status.data?.event_count ?? 0;

  return (
    <>
      <PageToolbar
        title="Dashboard"
        timeRange={timeRange}
        onTimeRangeChange={setTimeRange}
        filtersOpen={filtersOpen}
        onFiltersToggle={() => setFiltersOpen((o) => !o)}
        onRefresh={handleRefresh}
        onTourStart={startTour}
      >
        <StreamStatusPill stream={live} streamStalled={streamStalled} />
      </PageToolbar>

      <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-6">
        {filtersOpen && (
          <DashboardFilters
            agentFilter={agentFilter}
            statusFilter={statusFilter}
            onAgentChange={setAgentFilter}
            onStatusChange={setStatusFilter}
            onClear={() => {
              setAgentFilter("");
              setStatusFilter("");
            }}
          />
        )}

        {/* KPI strip */}
        <section className="grid grid-cols-2 lg:grid-cols-4 divide-y lg:divide-y-0 lg:divide-x divide-border/40 rounded-xl border border-border/40 bg-white dark:bg-zinc-900 shadow-sm overflow-hidden">
          <MetricCard
            title="Chain Height"
            value={chainHeight.toLocaleString()}
            unit="events"
            icon={Zap}
            pulse={live.connected}
          />
          <MetricCard
            title="Total Events"
            value={eventCount.toLocaleString()}
            unit="logged"
            icon={Activity}
          />
          <MetricCard
            title="Live Pushes"
            value={live.events.length}
            unit={live.connected ? "this session" : "offline"}
            icon={Wifi}
            pulse={live.connected && live.events.length > 0}
          />
          <MetricCard
            title="Tenant"
            value={status.data?.tenant_id ?? "-"}
            unit="org"
            icon={Database}
            valueClassName="text-sm font-mono break-all leading-snug"
            valueTitle={status.data?.tenant_id}
          />
        </section>

        {/* Two-column body */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2">
            <LiveFeed
              live={live.connected}
              timeRange={timeRange}
              agentId={agentFilter}
              status={statusFilter}
              refreshKey={refreshKey}
            />
          </div>

          <div className="space-y-6">
            <IntegrityCard chainHeight={chainHeight} connected={live.connected} lastEventAt={live.lastReceivedAt} />
            <IngestionPulseCard pulses={live.events.length} connected={live.connected} />
          </div>
        </div>
      </div>
    </>
  );
}

function DashboardFilters({
  agentFilter,
  statusFilter,
  onAgentChange,
  onStatusChange,
  onClear,
}: {
  agentFilter: string;
  statusFilter: ExecutionStatus | "";
  onAgentChange: (v: string) => void;
  onStatusChange: (v: ExecutionStatus | "") => void;
  onClear: () => void;
}) {
  const active = !!(agentFilter || statusFilter);

  return (
    <section className="flex flex-wrap items-end gap-3 rounded-xl border border-border/60 bg-card px-4 py-3">
      <label className="flex min-w-[10rem] flex-1 flex-col gap-1">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
          Agent ID
        </span>
        <input
          type="text"
          value={agentFilter}
          onChange={(e) => onAgentChange(e.target.value)}
          placeholder="Filter by agent…"
          className="h-8 rounded-md border border-border/70 bg-background px-2.5 text-xs font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-indigo-500/40"
        />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
          Status
        </span>
        <select
          value={statusFilter}
          onChange={(e) => onStatusChange(e.target.value as ExecutionStatus | "")}
          className="h-8 rounded-md border border-border/70 bg-background px-2.5 text-xs focus:outline-none focus:ring-1 focus:ring-indigo-500/40"
        >
          <option value="">All</option>
          {EXEC_STATUSES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>
      {active && (
        <button
          type="button"
          onClick={onClear}
          className="h-8 rounded-md px-2.5 text-[11px] font-medium text-zinc-500 hover:bg-muted/60 hover:text-foreground transition-colors"
        >
          Clear filters
        </button>
      )}
    </section>
  );
}

function StreamStatusPill({
  stream,
  streamStalled,
}: {
  stream: { connected: boolean; everOpened: boolean; gaveUp: boolean };
  streamStalled: boolean;
}) {
  let label = "Connecting…";
  if (stream.connected) label = "Real-time";
  else if (stream.gaveUp) label = "No live stream";
  else if (streamStalled && !stream.everOpened) label = "No live stream";
  else if (stream.everOpened) label = "Reconnecting…";

  const live = stream.connected;

  return (
    <span
      title={
        streamStalled && !stream.everOpened
          ? "SSE often breaks through the Next.js dev proxy. Set NEXT_PUBLIC_API_URL=http://localhost:8000 in .env.local to connect EventSource directly to the API."
          : undefined
      }
      className={cn(
        "inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[10px] font-semibold uppercase tracking-wider",
        live
          ? "text-emerald-700 dark:text-emerald-300 bg-emerald-500/10 border border-emerald-500/20"
          : "text-zinc-500 bg-muted border border-border",
      )}
    >
      <span
        className={cn(
          "size-1.5 rounded-full",
          live ? "bg-emerald-500 animate-pulse" : "bg-zinc-400",
        )}
      />
      {label}
    </span>
  );
}

function IntegrityCard({
  chainHeight,
  connected,
  lastEventAt,
}: {
  chainHeight: number;
  connected: boolean;
  lastEventAt: number | null;
}) {
  const [ago, setAgo] = useState<string>("Never");

  useEffect(() => {
    if (!lastEventAt) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setAgo("Never");
      return;
    }
    const update = () => {
      setAgo(`${Math.max(1, Math.floor((Date.now() - lastEventAt) / 1000))}s ago`);
    };
    update();
    const interval = setInterval(update, 1000);
    return () => clearInterval(interval);
  }, [lastEventAt]);

  return (
    <section className="rounded-xl border border-border/40 bg-white dark:bg-zinc-900 shadow-sm p-5 space-y-5">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <ShieldCheck className="size-3.5 text-zinc-400" />
          <h3 className="text-sm font-semibold text-foreground dark:text-white">
            Audit history
          </h3>
        </div>
        <span className="text-xs font-mono tabular-nums text-zinc-500">
          {chainHeight.toLocaleString()}
        </span>
      </header>

      <p className="text-xs text-zinc-500">{chainHeight.toLocaleString()} stored records. Open audit logs to verify the current chain; the live stream alone does not verify integrity.</p>
      <Link href="/dashboard/audit" className="text-xs underline">Verify stored history</Link>

      <div className="flex items-start gap-2.5 text-xs">
        <span
          className={cn(
            "size-1.5 rounded-full mt-1.5 shrink-0",
            connected ? "bg-emerald-500 animate-pulse" : "bg-zinc-400",
          )}
        />
        <div className="space-y-0.5 leading-relaxed">
          <p className="font-medium text-foreground/90">
            Capture stream status
          </p>
          <p className="text-[11px] text-zinc-500">
            Stream {connected ? "online" : "offline"} · last frame {ago}
          </p>
        </div>
      </div>
    </section>
  );
}

function IngestionPulseCard({
  pulses,
  connected,
}: {
  pulses: number;
  connected: boolean;
}) {
  const total = 40;
  return (
    <section className="rounded-xl border border-border/40 bg-white dark:bg-zinc-900 shadow-sm p-5 space-y-4">
      <header className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BarChart3 className="size-3.5 text-zinc-400" />
          <h3 className="text-sm font-semibold text-foreground dark:text-white">
            Ingestion Pulse
          </h3>
        </div>
        <span className="text-[10px] font-mono uppercase tracking-wider text-zinc-500 tabular-nums">
          {pulses}/{total}
        </span>
      </header>

      <div className="grid grid-cols-10 gap-1">
        {Array.from({ length: total }).map((_, i) => (
          <div
            key={i}
            className={cn(
              "aspect-square rounded-[3px] transition-colors",
              i < pulses
                ? connected
                  ? "bg-zinc-800 dark:bg-zinc-200"
                  : "bg-zinc-400 dark:bg-zinc-600"
                : "bg-zinc-100 dark:bg-zinc-800",
            )}
          />
        ))}
      </div>

      <p className="text-[11px] text-zinc-500">
        Each cell is one push this session.
      </p>
    </section>
  );
}
