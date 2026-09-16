"use client";

import { formatTraceDuration, type TraceTimeline } from "@/lib/trace-timeline";
import { SPAN_TYPE_COLORS, cn } from "@/lib/utils";

interface WaterfallChartProps {
  timeline: TraceTimeline;
  /** Selected span id; highlighted with a ring. */
  selectedSpanId?: string | null;
  /** Span ids on the critical path; rendered with an amber fill. */
  criticalSpanIds?: Set<string>;
  /** "As-of" timestamp from the scrubber. Spans entirely after this
   *  time are greyed out; bars in-progress at this time get a clamped
   *  width. Pass null/undefined to show every bar at full duration. */
  asOfMs?: number | null;
  onSpanClick?: (spanId: string) => void;
}

/**
 * WaterfallChart renders a horizontal flame-chart of spans ordered by
 * start time. Each row is one span; bar width is proportional to
 * duration. Hovering shows precise timing; clicking selects.
 */
export default function WaterfallChart({
  timeline,
  selectedSpanId,
  criticalSpanIds,
  asOfMs,
  onSpanClick,
}: WaterfallChartProps) {
  const { rows, totalMs } = timeline;
  const scaleMs = totalMs > 0 ? totalMs : 1;

  if (rows.length === 0) {
    return (
      <div className="text-xs text-zinc-500 px-4 py-6 text-center">
        No spans recorded yet.
      </div>
    );
  }

  // Time ruler ticks - five evenly spaced labels in addition to start/end.
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((p) => ({
    pct: p * 100,
    label: formatTraceDuration(p * totalMs),
  }));

  return (
    <div className="w-full">
      {/* Time ruler */}
      <div className="relative h-5 border-b border-border/60 text-[10px] font-mono text-zinc-500">
        {ticks.map((t) => (
          <span
            key={t.pct}
            className="absolute -translate-x-1/2 select-none"
            style={{ left: `${t.pct}%`, top: 0 }}
          >
            {t.label}
          </span>
        ))}
      </div>

      <div className="divide-y divide-border/40">
        {rows.map(({ span, startMs, endMs, durationMs }) => {
          const widthPct = ((endMs - startMs) / scaleMs) * 100;
          const leftPct = (startMs / scaleMs) * 100;
          const color = SPAN_TYPE_COLORS[span.span_type] || "#6b7280";
          const isSelected = selectedSpanId === span.id;
          const isCritical = !!criticalSpanIds?.has(span.id);
          // Scrubber: spans starting after asOfMs are dim; bars in
          // progress at asOfMs are clipped to that timestamp.
          let dim = false;
          let clippedWidthPct = widthPct;
          if (asOfMs != null) {
            if (startMs > asOfMs) {
              dim = true;
            } else if (endMs > asOfMs) {
              clippedWidthPct = ((asOfMs - startMs) / scaleMs) * 100;
            }
          }
          return (
            <button
              type="button"
              key={span.id}
              onClick={() => onSpanClick?.(span.id)}
              className={cn(
                "relative w-full grid grid-cols-[200px_1fr_60px] items-center gap-3 py-1 px-2 text-left transition-colors",
                isSelected ? "bg-muted/60" : "hover:bg-muted/30",
                dim && "opacity-30",
              )}
            >
              <div className="truncate flex items-center gap-2 text-[11px]">
                <span
                  className="size-2 rounded-sm shrink-0"
                  style={{ backgroundColor: color }}
                />
                <span className="truncate font-medium text-foreground">
                  {span.name}
                </span>
                <span className="text-[9px] uppercase tracking-widest text-zinc-500 shrink-0">
                  {span.span_type.replace(/_/g, " ")}
                </span>
              </div>
              <div className="relative h-3 bg-muted/30 rounded-sm">
                <div
                  className={cn(
                    "absolute top-0 h-full rounded-sm transition-all",
                    isSelected && "ring-1 ring-primary",
                  )}
                  style={{
                    left: `${leftPct}%`,
                    width: `${Math.max(0.5, clippedWidthPct)}%`,
                    backgroundColor: isCritical ? "#f59e0b" : color,
                    opacity: isCritical ? 0.9 : 0.75,
                  }}
                />
              </div>
              <div className="text-[10px] font-mono text-zinc-500 text-right tabular-nums">
                {formatTraceDuration(durationMs)}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
