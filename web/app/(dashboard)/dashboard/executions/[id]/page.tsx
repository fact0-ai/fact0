"use client";
import {
  ArrowLeft,
  Clock,
  Copy,
  Loader2,
  Pause,
  Play,
  RotateCcw,
  ShieldAlert,
  SkipBack,
  SkipForward,
} from "lucide-react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import WaterfallChart from "@/components/dag/waterfall-chart";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { RetentionBanner } from "@/components/dashboard/retention-banner";
import SpanInspector from "@/components/inspector/span-inspector";
import { useTelemetryClient } from "@/lib/api";
import { useDashboardHref } from "@/lib/demo/demo-context";
import { buildTraceTimeline, formatTraceDuration } from "@/lib/trace-timeline";
import { computeCriticalPath } from "@/lib/critical-path";
import type {
  DAGEdge,
  DAGNode,
  Execution,
  ExecutionEvent,
  ReplayFrame,
  Span,
} from "@/lib/types";
import {
  STATUS_COLORS,
  cn,
  formatDuration,
  formatTimestamp,
} from "@/lib/utils";
// Avoid hydration issues - React Flow needs window.
const ExecutionDAG = dynamic(() => import("@/components/dag/execution-dag"), {
  ssr: false,
  loading: () => (
    <div className="w-full h-full flex items-center justify-center bg-muted/20 rounded-xl border border-border/60">
      <Loader2 className="size-5 text-muted-foreground animate-spin" />
    </div>
  ),
});
export default function ExecutionDetailPage() {
  const params = useParams();
  const executionId = params.id as string;
  const { client, ready, orgId } = useTelemetryClient();
  const executionsBase = useDashboardHref("/executions");
  const [execution, setExecution] = useState<Execution | null>(null);
  const [spans, setSpans] = useState<Span[]>([]);
  const [dagNodes, setDagNodes] = useState<DAGNode[]>([]);
  const [dagEdges, setDagEdges] = useState<DAGEdge[]>([]);
  const [rawFrames, setFrames] = useState<ReplayFrame[]>([]);
  const [reportedDurationMs, setReportedDurationMs] = useState(0);
  const [snapshotAt, setSnapshotAt] = useState(() => Date.now());
  const [selectedSpan, setSelectedSpan] = useState<Span | null>(null);
  const [spanEvents, setSpanEvents] = useState<ExecutionEvent[]>([]);
  const [currentFrame, setCurrentFrame] = useState(0);
  const [isPlaying, setIsPlaying] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const initialSelectionDone = useRef(false);
  useEffect(() => {
    initialSelectionDone.current = false;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCurrentFrame(0);
    setIsPlaying(false);
    setSelectedSpan(null);
    setSpanEvents([]);
  }, [executionId]);
  useEffect(() => {
    if (!ready || !orgId) return;
    let cancelled = false;
    async function load() {
      setLoading(true);
      setError(null);
      try {
        {
          const [execData, spansData, dagData, replayData] = await Promise.all([
            client.getExecution(executionId),
            client.getSpans(executionId),
            client.getExecutionDAG(executionId),
            client.replayExecution(executionId),
          ]);
          if (cancelled) return;
          setExecution(execData);
          setSpans(spansData.spans || []);
          setDagNodes(dagData.nodes || []);
          setDagEdges(dagData.edges || []);
          setFrames(replayData.frames || []);
          setReportedDurationMs(replayData.duration_ms ?? 0);
          setSnapshotAt(Date.now());
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load execution",
          );
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    load();
    return () => {
      cancelled = true;
    };
  }, [executionId, client, ready, orgId]);
  const timeline = useMemo(
    () =>
      buildTraceTimeline({
        executionStart: execution?.started_at,
        executionEnd: execution?.ended_at,
        spans,
        frames: rawFrames,
        reportedDurationMs,
        now: snapshotAt,
      }),
    [execution, spans, rawFrames, reportedDurationMs, snapshotAt],
  );
  const frames = timeline.frames;
  // Critical path is purely structural - derive it once per DAG payload.
  const criticalSpanIds = useMemo(
    () => computeCriticalPath(dagNodes, dagEdges),
    [dagNodes, dagEdges],
  );
  // Redacted pill - set on any span whose ingestion-time redaction
  // marker fired.
  const wasRedacted = useMemo(
    () => spans.some((s) => s.metadata?.redacted === "true"),
    [spans],
  );
  const handleNodeClick = useCallback(
    async (spanId: string) => {
      const span = spans.find((s) => s.id === spanId);
      if (!span) return;
      setSelectedSpan(span);
      try {
        const data = await client.getSpanEvents(spanId);
        setSpanEvents(data.events || []);
      } catch {
        setSpanEvents([]);
      }
    },
    [spans, client],
  );
  // Default to the earliest span so the inspector isn't empty on first open.
  useEffect(() => {
    if (initialSelectionDone.current || spans.length === 0) return;
    const first = [...spans].sort(
      (a, b) =>
        new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
    )[0];
    if (!first) return;
    initialSelectionDone.current = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void handleNodeClick(first.id);
  }, [spans, handleNodeClick]);
  // Playback timer - bounded delay so very long/short gaps don't lock
  // the UI or fast-forward illegibly.
  useEffect(() => {
    if (!isPlaying || frames.length === 0) return;
    const delay = Math.min(
      Math.max(frames[currentFrame + 1]?.delta_ms || 100, 50),
      500,
    );
    const t = setTimeout(() => {
      if (currentFrame < frames.length - 1) setCurrentFrame((p) => p + 1);
      else setIsPlaying(false);
    }, delay);
    return () => clearTimeout(t);
  }, [isPlaying, currentFrame, frames]);
  const totalDurationMs = timeline.totalMs;
  // Spans whose lifespan brackets the current scrubber position.
  const activeSpanIds = useMemo(() => {
    if (frames.length === 0) return new Set<string>();
    const active = new Set<string>();
    for (let i = 0; i <= currentFrame; i++) {
      const f = frames[i];
      if (f.event_type === "SPAN_STARTED") active.add(f.span_id);
      if (f.event_type === "SPAN_ENDED" || f.event_type === "SPAN_FAILED") {
        active.delete(f.span_id);
      }
    }
    return active;
  }, [frames, currentFrame]);
  const asOfMs =
    frames.length > 0 && currentFrame < frames.length
      ? frames[currentFrame].elapsed_ms
      : null;
  const onCopyId = useCallback(() => {
    if (!execution) return;
    navigator.clipboard.writeText(execution.id).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [execution]);
  // ─── Loading ─────────────────────────────────────────────────────────
  if (loading) {
    return (
      <>
        <PageToolbar title="Execution Detail" showFilters={false} />
        <div className="p-8 flex items-center justify-center text-xs text-zinc-500">
          <Loader2 className="size-3.5 animate-spin mr-2" />
          Loading trace…
        </div>
      </>
    );
  }
  if (error || !execution) {
    return (
      <>
        <PageToolbar title="Execution Detail" showFilters={false} />
        <div className="p-8 space-y-4">
          <div className="rounded-xl border border-destructive/30 bg-destructive/5 p-5 text-sm text-destructive">
            {error || "Execution not found"}
          </div>
          <Link
            href={executionsBase}
            className="text-xs text-muted-foreground hover:text-foreground"
          >
            ← Back to executions
          </Link>
        </div>
      </>
    );
  }
  const statusColor = STATUS_COLORS[execution.status] || "#6b7280";
  const durationLabel =
    execution.ended_at && execution.started_at
      ? formatDuration(
          new Date(execution.ended_at).getTime() -
            new Date(execution.started_at).getTime(),
        )
      : execution.status === "RUNNING"
        ? "In progress"
        : "-";
  // ─── Main layout ─────────────────────────────────────────────────────
  return (
    <>
      <PageToolbar title="Execution Detail" showFilters={false}>
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <Link
            href={executionsBase}
            className="inline-flex items-center gap-1.5 text-[11px] font-medium text-zinc-500 hover:text-foreground transition-colors"
          >
            <ArrowLeft className="size-3.5" />
            All executions
          </Link>
          <div className="flex items-center gap-2">
            {wasRedacted && (
              <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[10px] font-semibold uppercase tracking-wider bg-amber-500/10 text-amber-700 dark:text-amber-300 border border-amber-500/20">
                <ShieldAlert className="size-3" />
                Redacted
              </span>
            )}
            <button
              type="button"
              onClick={onCopyId}
              className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-medium text-zinc-500 hover:text-foreground hover:bg-muted/60 transition-colors"
            >
              <Copy className="size-3" />
              {copied ? "Copied" : "Copy ID"}
            </button>
          </div>
        </div>
      </PageToolbar>

      <div className="relative animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-4">
        <div className={cn("space-y-4 transition-all duration-300", false)}>
          <RetentionBanner />

          {/* Header strip */}
          <div className="rounded-xl border border-border/60 bg-card p-5 grid grid-cols-2 lg:grid-cols-5 gap-4">
            <div className="space-y-1 lg:col-span-2">
              <div className="text-[10px] uppercase tracking-widest text-zinc-500">
                Agent
              </div>
              <div className="flex items-center gap-2">
                <span
                  className="size-2 rounded-full shrink-0"
                  style={{ backgroundColor: statusColor }}
                />
                <span className="font-bold text-sm text-foreground truncate">
                  {execution.agent_name || execution.agent_id}
                </span>
                <span
                  className="text-[9px] font-bold uppercase tracking-widest px-1.5 py-0.5 rounded"
                  style={{
                    backgroundColor: `${statusColor}18`,
                    color: statusColor,
                  }}
                >
                  {execution.status}
                </span>
              </div>
              <div className="text-[10px] font-mono text-zinc-500 truncate">
                {execution.id}
              </div>
            </div>
            <Stat label="Trigger" value={execution.trigger || "-"} />
            <Stat
              label="Started"
              value={formatTimestamp(execution.started_at)}
            />
            <Stat label="Duration" value={durationLabel} />
          </div>

          {/* DAG + Inspector side-by-side */}
          <div className="grid grid-cols-1 lg:grid-cols-[1fr_320px] gap-4">
            <div
              data-tour="execution-graph"
              className="rounded-xl border border-border/60 bg-card overflow-hidden"
            >
              <div className="px-4 py-2.5 border-b border-border/60 flex items-center justify-between">
                <div className="text-[11px] font-bold uppercase tracking-widest text-zinc-500">
                  Execution graph
                </div>
                <div className="flex items-center gap-3 text-[10px] text-zinc-500">
                  <Legend color="#f59e0b" label="Critical path" />
                  <Legend color="#6366f1" label="Live" />
                </div>
              </div>
              <div className="h-[min(520px,55vh)] min-h-[360px]">
                {
                  <ExecutionDAG
                    dagNodes={dagNodes}
                    dagEdges={dagEdges}
                    onNodeClick={handleNodeClick}
                    selectedSpanId={selectedSpan?.id}
                    activeSpanIds={activeSpanIds}
                    criticalSpanIds={criticalSpanIds}
                  />
                }
              </div>
            </div>

            <div className="rounded-xl border border-border/60 bg-card overflow-hidden min-h-[300px]">
              {selectedSpan ? (
                <SpanInspector
                  span={selectedSpan}
                  events={spanEvents}
                  onClose={() => {
                    setSelectedSpan(null);
                    setSpanEvents([]);
                  }}
                />
              ) : (
                <div className="h-full flex items-center justify-center px-6 text-center text-xs text-zinc-500">
                  Select any node in the graph or any row in the waterfall to
                  inspect its span detail, events, and raw payload.
                </div>
              )}
            </div>
          </div>

          {/* Timing waterfall */}
          <div
            data-tour="waterfall-chart"
            className="rounded-xl border border-border/60 bg-card overflow-hidden"
          >
            <div className="px-4 py-2.5 border-b border-border/60 flex items-center justify-between">
              <div className="text-[11px] font-bold uppercase tracking-widest text-zinc-500">
                Timing waterfall
              </div>
              <div className="text-[10px] text-zinc-500 tabular-nums flex items-center gap-1.5">
                <Clock className="size-3" />
                {
                  <span>
                    {spans.length} {spans.length === 1 ? "span" : "spans"} ·{" "}
                    {formatTraceDuration(totalDurationMs)}
                  </span>
                }
              </div>
            </div>
            <div className="p-3">
              {
                <WaterfallChart
                  timeline={timeline}
                  selectedSpanId={selectedSpan?.id}
                  criticalSpanIds={criticalSpanIds}
                  asOfMs={asOfMs}
                  onSpanClick={handleNodeClick}
                />
              }
            </div>
          </div>

          {timeline.beforeExecutionMs > 0 && (
            <p className="text-xs text-muted-foreground">
              Some captured timestamps precede the execution start by{" "}
              {formatTraceDuration(timeline.beforeExecutionMs)}. The timeline
              starts at the earliest captured timestamp; offsets reflect the
              source clocks.
            </p>
          )}

          {/* Time scrubber */}
          {frames.length > 0 && (
            <div className="rounded-xl border border-border/60 bg-card p-4 space-y-3">
              <div className="flex items-center gap-2 text-[11px] uppercase tracking-widest text-zinc-500">
                <button
                  onClick={() => {
                    setCurrentFrame(0);
                    setIsPlaying(false);
                  }}
                  disabled={currentFrame === 0}
                  className="p-1.5 rounded-md hover:bg-muted/60 disabled:opacity-30"
                  title="Reset"
                >
                  <RotateCcw className="size-3.5" />
                </button>
                <button
                  onClick={() => setCurrentFrame(Math.max(0, currentFrame - 1))}
                  disabled={currentFrame === 0}
                  className="p-1.5 rounded-md hover:bg-muted/60 disabled:opacity-30"
                >
                  <SkipBack className="size-3.5" />
                </button>
                <button
                  onClick={() => setIsPlaying((p) => !p)}
                  className={cn(
                    "p-2 rounded-md transition-colors",
                    isPlaying
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted/60 hover:bg-muted",
                  )}
                >
                  {isPlaying ? (
                    <Pause className="size-3.5" />
                  ) : (
                    <Play className="size-3.5" />
                  )}
                </button>
                <button
                  onClick={() =>
                    setCurrentFrame(
                      Math.min(frames.length - 1, currentFrame + 1),
                    )
                  }
                  disabled={currentFrame >= frames.length - 1}
                  className="p-1.5 rounded-md hover:bg-muted/60 disabled:opacity-30"
                >
                  <SkipForward className="size-3.5" />
                </button>
                <div className="flex-1" />
                <span className="font-mono tabular-nums normal-case tracking-normal">
                  Frame {currentFrame + 1} of {frames.length} ·{" "}
                  {formatTraceDuration(asOfMs ?? 0)} /{" "}
                  {formatTraceDuration(totalDurationMs)}
                </span>
              </div>
              <input
                aria-label="Replay frame"
                type="range"
                min={0}
                max={Math.max(0, frames.length - 1)}
                value={currentFrame}
                onChange={(e) => {
                  setIsPlaying(false);
                  setCurrentFrame(Number(e.target.value));
                }}
                className="w-full accent-indigo-500"
              />
            </div>
          )}
        </div>

        {false}
      </div>
    </>
  );
}
function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1 min-w-0">
      <div className="text-[10px] uppercase tracking-widest text-zinc-500">
        {label}
      </div>
      <div className="text-sm font-medium text-foreground truncate">
        {value}
      </div>
    </div>
  );
}
function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="size-2 rounded-sm" style={{ backgroundColor: color }} />
      {label}
    </span>
  );
}
