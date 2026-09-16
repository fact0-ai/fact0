import type { ReplayFrame, Span } from "./types";

/** Date.parse discards fractional milliseconds; retain them for short tool spans. */
export function traceTimestampMs(value?: string): number | null {
  if (!value) return null;
  const milliseconds = Date.parse(value);
  if (!Number.isFinite(milliseconds)) return null;
  const fraction = value.match(/\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/i)?.[1];
  const remainder =
    fraction && fraction.length > 3 ? Number(`0.${fraction.slice(3)}`) : 0;
  return milliseconds + remainder;
}

export interface TraceTimeline {
  startMs: number;
  endMs: number;
  totalMs: number;
  beforeExecutionMs: number;
  rows: { span: Span; startMs: number; endMs: number; durationMs: number }[];
  frames: ReplayFrame[];
}

/** Use a single captured time envelope for bars, frame offsets and replay duration. */
export function buildTraceTimeline({
  executionStart,
  executionEnd,
  spans,
  frames = [],
  reportedDurationMs = 0,
  now,
}: {
  executionStart?: string;
  executionEnd?: string;
  spans: Span[];
  frames?: ReplayFrame[];
  reportedDurationMs?: number;
  now: number;
}): TraceTimeline {
  const executionStartMs = traceTimestampMs(executionStart);
  const executionEndMs = traceTimestampMs(executionEnd);
  const frameTimes = frames.map(
    (frame) =>
      traceTimestampMs(frame.event?.timestamp) ??
      (executionStartMs === null
        ? null
        : executionStartMs + Math.max(0, frame.elapsed_ms || 0)),
  );
  const starts = [
    executionStartMs,
    ...spans.map((span) => traceTimestampMs(span.started_at)),
    ...frameTimes,
  ].filter((value): value is number => value !== null);
  const startMs = starts.length ? Math.min(...starts) : now;
  const ends = [
    ...starts,
    startMs,
    executionEndMs,
    ...spans.map((span) => traceTimestampMs(span.ended_at)),
    ...frameTimes,
  ].filter((value): value is number => value !== null);
  if (
    executionStartMs !== null &&
    Number.isFinite(reportedDurationMs) &&
    reportedDurationMs > 0
  )
    ends.push(executionStartMs + reportedDurationMs);
  if (executionEndMs === null) ends.push(now);
  const endMs = Math.max(...ends);
  const totalMs = Math.max(0, endMs - startMs);
  const rows = spans
    .map((span) => {
      const spanStart = traceTimestampMs(span.started_at) ?? startMs;
      // A reversed/missing endpoint cannot produce a negative duration or bar.
      const spanEnd = Math.max(
        spanStart,
        traceTimestampMs(span.ended_at) ?? endMs,
      );
      const offset = Math.max(0, spanStart - startMs);
      const durationMs = Math.max(0, spanEnd - spanStart);
      return { span, startMs: offset, endMs: offset + durationMs, durationMs };
    })
    .sort((a, b) => a.startMs - b.startMs);
  const ordered = frames
    .map((frame, index) => ({ frame, time: frameTimes[index] ?? startMs }))
    .sort(
      (a, b) =>
        a.time - b.time || a.frame.sequence_number - b.frame.sequence_number,
    );
  const normalizedFrames = ordered.map(({ frame, time }, index) => ({
    ...frame,
    elapsed_ms: Math.max(0, time - startMs),
    delta_ms: Math.max(0, time - (ordered[index - 1]?.time ?? startMs)),
  }));
  return {
    startMs,
    endMs,
    totalMs,
    rows,
    frames: normalizedFrames,
    beforeExecutionMs:
      executionStartMs === null ? 0 : Math.max(0, executionStartMs - startMs),
  };
}

export function formatTraceDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "0ms";
  if (ms < 1) return "<1ms";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}
