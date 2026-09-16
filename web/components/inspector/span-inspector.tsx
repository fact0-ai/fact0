"use client";

import { useState } from "react";
import { formatTraceDuration, traceTimestampMs } from "@/lib/trace-timeline";
import type { Span, ExecutionEvent } from "@/lib/types";
import { SPAN_TYPE_COLORS, STATUS_COLORS, formatDuration, formatTimestamp } from "@/lib/utils";
import { X, Clock, Tag, AlertTriangle, Wrench, Brain, Database, UserCheck, ShieldCheck, Check } from "lucide-react";
import { IOModeToggle, LLMPayloadBlock, type IOViewMode } from "./llm-io-view";

interface SpanInspectorProps {
  span: Span;
  events: ExecutionEvent[];
  onClose: () => void;
}

export default function SpanInspector({ span, events, onClose }: SpanInspectorProps) {
  const [ioMode, setIoMode] = useState<IOViewMode>("pretty");
  const borderColor = SPAN_TYPE_COLORS[span.span_type] || "#6b7280";
  const statusColor = STATUS_COLORS[span.status] || "#6b7280";
  const durationMs = span.ended_at
    ? Math.max(0, (traceTimestampMs(span.ended_at) ?? 0) - (traceTimestampMs(span.started_at) ?? 0))
    : 0;

  const SpanIcon =
    {
      TOOL_CALL: Wrench,
      MODEL_INVOCATION: Brain,
      STATE_MUTATION: Database,
      HUMAN_APPROVAL: UserCheck,
      POLICY_EVALUATION: ShieldCheck,
      CUSTOM: Tag,
    }[span.span_type] || Tag;

  const isSlow = durationMs > 5000;
  const isHighTokens = span.model_invocation && span.model_invocation.total_tokens > 4096;
  const isTruncated = span.metadata && span.metadata["ai.response.finishReason"] === "length";

  return (
    <div className="h-full flex flex-col bg-card overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <SpanIcon className="size-4 shrink-0" style={{ color: borderColor }} />
          <h3 className="text-sm font-black tracking-tight text-foreground truncate">{span.name}</h3>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-muted rounded-lg transition-colors text-muted-foreground hover:text-foreground ml-2 shrink-0"
        >
          <X className="size-4" />
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* Status + Type badges */}
        <div className="flex items-center gap-2 flex-wrap">
          <span
            className="inline-flex items-center px-2 py-0.5 rounded-lg text-[9px] font-black uppercase tracking-wider"
            style={{ backgroundColor: `${statusColor}18`, color: statusColor }}
          >
            {span.status}
          </span>
          <span
            className="inline-flex items-center px-2 py-0.5 rounded-lg text-[9px] font-black uppercase tracking-wider"
            style={{ backgroundColor: `${borderColor}18`, color: borderColor }}
          >
            {span.span_type.replace(/_/g, " ")}
          </span>
        </div>

        {/* Timing */}
        <div className="space-y-1">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Clock className="size-3" />
            <span>{span.ended_at ? formatTraceDuration(durationMs) : "In progress"}</span>
          </div>
          <div className="text-[10px] text-muted-foreground/70 font-mono">
            {formatTimestamp(span.started_at)}
          </div>
          {span.ended_at && (
            <div className="text-[10px] text-muted-foreground/70 font-mono">
              → {formatTimestamp(span.ended_at)}
            </div>
          )}
        </div>

        {/* Error */}
        {span.error && (
          <div className="p-3 bg-destructive/5 border border-destructive/20 rounded-xl">
            <div className="flex items-center gap-1.5 text-xs text-destructive font-bold mb-1">
              <AlertTriangle className="size-3" />
              {span.error.code}
            </div>
            <p className="text-xs text-destructive/80">{span.error.message}</p>
            {span.error.stack_trace && (
              <pre className="mt-2 text-[10px] text-destructive/60 overflow-x-auto font-mono leading-relaxed">
                {span.error.stack_trace}
              </pre>
            )}
          </div>
        )}

        {/* Tool Call */}
        {span.tool_call && (
          <DetailSection title="Tool Call" color={borderColor}>
            <DetailRow label="Tool" value={span.tool_call.tool_name} />
            {span.tool_call.tool_version && <DetailRow label="Version" value={span.tool_call.tool_version} />}
            <DetailRow label="Duration" value={formatDuration(span.tool_call.duration_ms)} />
            {span.tool_call.input?.inline != null && <DetailJSON label="Input" data={span.tool_call.input.inline} />}
            {span.tool_call.output?.inline != null && <DetailJSON label="Output" data={span.tool_call.output.inline} />}
          </DetailSection>
        )}

        {/* Model Invocation */}
        {span.model_invocation && (
          <DetailSection title="Model Invocation" color={borderColor}>
            <DetailRow label="Model" value={span.model_invocation.model_name} />
            <DetailRow label="Provider" value={span.model_invocation.model_provider} />
            <DetailRow
              label="Tokens"
              value={`${span.model_invocation.total_tokens} (${span.model_invocation.prompt_tokens}p / ${span.model_invocation.completion_tokens}c)`}
            />
            {span.model_invocation.cost_usd != null && span.model_invocation.cost_usd > 0 && (
              <DetailRow label="Cost" value={formatCostUSD(span.model_invocation.cost_usd)} />
            )}
            <DetailRow label="Latency" value={formatDuration(span.model_invocation.latency_ms || durationMs)} />
            {span.model_invocation.temperature != null && (
              <DetailRow label="Temp" value={String(span.model_invocation.temperature)} />
            )}
            {span.model_invocation.prompt_name && (
              <DetailRow
                label="Prompt"
                value={`${span.model_invocation.prompt_name}${
                  span.model_invocation.prompt_version ? ` v${span.model_invocation.prompt_version}` : ""
                }`}
              />
            )}
            {span.model_invocation.session_id && (
              <DetailRow label="Session" value={span.model_invocation.session_id} />
            )}

            {/* Input / Output */}
            {(span.model_invocation.prompt || span.model_invocation.completion) && (
              <div className="mt-3 pt-3 border-t border-border/60">
                <div className="flex items-center justify-between mb-1">
                  <h5 className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
                    Input / Output
                  </h5>
                  <IOModeToggle mode={ioMode} onChange={setIoMode} />
                </div>
                {span.model_invocation.prompt && (
                  <LLMPayloadBlock label="Input" payload={span.model_invocation.prompt} mode={ioMode} />
                )}
                {span.model_invocation.completion && (
                  <LLMPayloadBlock label="Output" payload={span.model_invocation.completion} mode={ioMode} />
                )}
              </div>
            )}

            {/* Auto-Evaluations Badges */}
            <div className="mt-4 pt-3 border-t border-border/60 space-y-2">
              <h5 className="text-[9px] font-black uppercase tracking-widest text-zinc-500">Auto-Evaluations</h5>
              <div className="space-y-1.5">
                <div className="flex items-center justify-between text-xs">
                  <span className="text-zinc-500">Latency Budget</span>
                  {isSlow ? (
                    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-amber-500 bg-amber-500/10 px-1.5 py-0.5 rounded">
                      <AlertTriangle className="size-3" />
                      Slow (&gt;5s)
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-emerald-500 bg-emerald-500/10 px-1.5 py-0.5 rounded">
                      <Check className="size-3" />
                      Pass (&lt;5s)
                    </span>
                  )}
                </div>
                <div className="flex items-center justify-between text-xs">
                  <span className="text-zinc-500">Token Budget</span>
                  {isHighTokens ? (
                    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-amber-500 bg-amber-500/10 px-1.5 py-0.5 rounded">
                      <AlertTriangle className="size-3" />
                      High (&gt;4k)
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-emerald-500 bg-emerald-500/10 px-1.5 py-0.5 rounded">
                      <Check className="size-3" />
                      Pass (&lt;4k)
                    </span>
                  )}
                </div>
                <div className="flex items-center justify-between text-xs">
                  <span className="text-zinc-500">Completion Quality</span>
                  {isTruncated ? (
                    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-red-500 bg-red-500/10 px-1.5 py-0.5 rounded">
                      <AlertTriangle className="size-3" />
                      Truncated
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-emerald-500 bg-emerald-500/10 px-1.5 py-0.5 rounded">
                      <Check className="size-3" />
                      Complete
                    </span>
                  )}
                </div>
              </div>
            </div>
          </DetailSection>
        )}

        {/* State Mutation */}
        {span.state_mutation && (
          <DetailSection title="State Mutation" color={borderColor}>
            <DetailRow label="Key" value={span.state_mutation.key} />
            <DetailRow label="Type" value={span.state_mutation.mutation_type} />
          </DetailSection>
        )}

        {/* Human Approval */}
        {span.human_approval && (
          <DetailSection title="Human Approval" color={borderColor}>
            <DetailRow label="Approver" value={span.human_approval.approver_name} />
            <DetailRow label="Decision" value={span.human_approval.decision} />
            {span.human_approval.reasoning && <DetailRow label="Reasoning" value={span.human_approval.reasoning} />}
          </DetailSection>
        )}

        {/* Policy Evaluation */}
        {span.policy_evaluation && (
          <DetailSection title="Policy Evaluation" color={borderColor}>
            <DetailRow label="Policy" value={span.policy_evaluation.policy_name} />
            <DetailRow label="Result" value={span.policy_evaluation.result} />
            {(span.policy_evaluation.violations?.length ?? 0) > 0 && (
              <div className="mt-1 space-y-0.5">
                <span className="text-[9px] text-muted-foreground uppercase tracking-wider">Violations</span>
                {(span.policy_evaluation.violations ?? []).map((v, i) => (
                  <div key={i} className="text-xs text-destructive font-mono">• {v}</div>
                ))}
              </div>
            )}
          </DetailSection>
        )}

        {/* Metadata */}
        {span.metadata && Object.keys(span.metadata).length > 0 && (
          <DetailSection title="Metadata" color="#6b7280">
            {Object.entries(span.metadata).map(([k, v]) => (
              <DetailRow key={k} label={k} value={String(v)} />
            ))}
          </DetailSection>
        )}

        {/* Events */}
        {events.length > 0 && (
          <div className="space-y-2">
            <h4 className="text-[9px] font-black text-muted-foreground uppercase tracking-widest">
              Events ({events.length})
            </h4>
            <div className="space-y-1">
              {events.map((evt) => (
                <div key={evt.id} className="p-2 bg-muted/50 rounded-lg border border-border">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-mono text-foreground">{evt.event_type}</span>
                    <span className="text-[9px] text-muted-foreground font-mono">#{evt.sequence_number}</span>
                  </div>
                  <div className="text-[9px] text-muted-foreground mt-0.5">{formatTimestamp(evt.timestamp)}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* IDs */}
        <div className="space-y-1 pt-3 border-t border-border">
          <div className="text-[9px] text-muted-foreground/60 font-mono break-all">ID: {span.id}</div>
          {span.parent_span_id && (
            <div className="text-[9px] text-muted-foreground/60 font-mono break-all">
              Parent: {span.parent_span_id}
            </div>
          )}
          {span.caused_by_span_ids && span.caused_by_span_ids.length > 0 && (
            <div className="text-[9px] text-muted-foreground/60 font-mono break-all">
              Caused by: {span.caused_by_span_ids.join(", ")}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function formatCostUSD(cost: number): string {
  if (cost >= 0.01) return `$${cost.toFixed(4)}`;
  return `$${cost.toPrecision(3)}`;
}

// ─── Detail helpers ───────────────────────────────────────────────────────────

function DetailSection({
  title,
  color,
  children,
}: {
  title: string;
  color: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className="p-3 rounded-xl border border-border"
      style={{ borderLeftColor: color, borderLeftWidth: 3 }}
    >
      <h4 className="text-[9px] font-black text-muted-foreground uppercase tracking-widest mb-2">{title}</h4>
      <div className="space-y-1">{children}</div>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <span className="text-[9px] text-muted-foreground uppercase shrink-0 tracking-wider">{label}:</span>
      <span className="text-xs text-foreground font-mono break-all">{value}</span>
    </div>
  );
}

function DetailJSON({ label, data }: { label: string; data: unknown }) {
  return (
    <div className="mt-1">
      <span className="text-[9px] text-muted-foreground uppercase tracking-wider">{label}:</span>
      <pre className="mt-0.5 text-[10px] text-foreground/80 bg-muted rounded-lg p-2 overflow-x-auto font-mono max-h-40 border border-border">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}
