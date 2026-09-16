"use client";

import {
  Brain,
  Cpu,
  DollarSign,
  Flame,
  Hourglass,
  Percent,
  Workflow
} from "lucide-react";
import { useState } from "react";

import { DashboardSkeleton } from "@/components/dashboard/dashboard-skeleton";
import { MetricCard } from "@/components/dashboard/metric-card";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { docsHref } from "@/lib/docs-origin";
import { useErrorMetrics, useLLMMetrics, useToolMetrics } from "@/lib/use-analytics";
import { useTour } from "@/lib/use-tour";
import { cn } from "@/lib/utils";

// Custom SVG Area/Line Chart Component for High-Fidelity Visuals
function CustomAreaChart({
  data,
  xKey,
  yKey,
  colorClass = "text-indigo-500",
  fillColorClass = "fill-indigo-500/10",
  strokeColor = "currentColor"
}: {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  data: any[];
  xKey: string;
  yKey: string;
  colorClass?: string;
  fillColorClass?: string;
  strokeColor?: string;
}) {
  if (!data || data.length === 0) return <div className="h-32 flex items-center justify-center text-xs text-zinc-500">No data available</div>;

  const values = data.map((d) => d[yKey]);
  const maxValue = Math.max(...values, 10);
  const minValue = Math.min(...values, 0);
  const range = maxValue - minValue;

  const width = 500;
  const height = 120;
  const padding = 10;

  const points = data.map((d, index) => {
    const x = padding + (index / (data.length - 1)) * (width - padding * 2);
    const y = height - padding - ((d[yKey] - minValue) / range) * (height - padding * 2);
    return { x, y, val: d[yKey], label: d[xKey] };
  });

  const pathD = points.reduce((acc, p, i) => {
    return acc + `${i === 0 ? "M" : "L"} ${p.x} ${p.y}`;
  }, "");

  const areaD = pathD + ` L ${points[points.length - 1].x} ${height - padding} L ${points[0].x} ${height - padding} Z`;

  return (
    <div className="relative group w-full">
      <svg viewBox={`0 0 ${width} ${height}`} className={cn("w-full h-32 overflow-visible", colorClass)}>
        {/* Grid lines */}
        <line x1={padding} y1={height - padding} x2={width - padding} y2={height - padding} className="stroke-zinc-800" strokeWidth={1} />
        <line x1={padding} y1={padding} x2={width - padding} y2={padding} className="stroke-zinc-800/40" strokeDasharray="3 3" strokeWidth={1} />

        {/* Area fill */}
        <path d={areaD} className={fillColorClass} />

        {/* Stroke line */}
        <path d={pathD} fill="none" stroke={strokeColor} strokeWidth={2} className="transition-all duration-300" />

        {/* Mini dot points */}
        {points.length <= 15 && points.map((p, i) => (
          <circle
            key={i}
            cx={p.x}
            cy={p.y}
            r={3}
            className="fill-background stroke-current stroke-2 hover:r-5 cursor-pointer transition-all duration-150"
          >
            <title>{`${p.label}: ${p.val}`}</title>
          </circle>
        ))}
      </svg>
      {/* Simple X axis labels */}
      <div className="flex justify-between px-1 text-[9px] text-zinc-500 font-mono mt-1">
        <span>{data[0][xKey]}</span>
        <span>{data[Math.floor(data.length / 2)][xKey]}</span>
        <span>{data[data.length - 1][xKey]}</span>
      </div>
    </div>
  );
}

interface EmptyStateProps {
  title: string;
  description: string;
  tabType: "llm" | "tools" | "errors";
}

function ObservabilityEmptyState({ title, description, tabType }: EmptyStateProps) {
  const codeSnippet = tabType === "llm"
    ? `from fact0 import Client
from fact0.integrations.langchain import Fact0CallbackHandler

# 1. Initialize Fact0 Client
client = Client(api_key="your_api_key")

# 2. Attach Telemetry Callback Handler to your LangChain Agent
handler = Fact0CallbackHandler(client=client, agent_id="my-agent")
response = agent.run("What's the weather today?", callbacks=[handler])`
    : tabType === "tools"
      ? `# Use standard python SDK telemetry decorator
from fact0 import Client, telemetry

client = Client(api_key="your_api_key")

@telemetry.tool(client=client, name="db_query_tool")
def search_database(query: str):
    # Your database query logic here
    return results`
      : `# Capture errors and failures automatically
try:
    # Run agent execution
    result = agent.run(task)
except Exception as e:
    # Fact0 captures uncaught exceptions and failed steps automatically
    # as status="FAILED" inside the execution spans.
    raise e`;

  return (
    <div className="rounded-xl border border-dashed border-border bg-card/50 p-8 text-center max-w-2xl mx-auto my-6 space-y-6 animate-in fade-in duration-300">
      <div className="mx-auto size-12 rounded-xl bg-indigo-500/5 border border-indigo-500/10 flex items-center justify-center">
        {tabType === "llm" ? (
          <Brain className="size-6 text-indigo-500 animate-pulse" />
        ) : tabType === "tools" ? (
          <Workflow className="size-6 text-violet-500" />
        ) : (
          <Flame className="size-6 text-red-500" />
        )}
      </div>

      <div className="space-y-2">
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        <p className="text-xs text-muted-foreground leading-relaxed max-w-md mx-auto">
          {description}
        </p>
      </div>

      <div className="text-left space-y-2 max-w-lg mx-auto">
        <span className="text-[10px] uppercase tracking-wider font-bold text-zinc-500">Quick Integration Example</span>
        <div className="rounded-lg bg-zinc-950 p-4 border border-zinc-800 font-mono text-[11px] text-zinc-300 overflow-x-auto leading-relaxed shadow-sm">
          <pre>{codeSnippet}</pre>
        </div>
      </div>

      <div className="flex items-center justify-center gap-3 pt-2">
        <a
          href="/docs"
          className="text-xs font-semibold px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white transition-colors"
        >
          View SDK Documentation
        </a>
      </div>
    </div>
  );
}

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

export default function ObservabilityHubPage() {
  return (
    <Suspense fallback={<DashboardSkeleton />}>
      <ObservabilityHubPageContent />
    </Suspense>
  );
}

function ObservabilityHubPageContent() {
  const searchParams = useSearchParams();
  const defaultTab = (searchParams.get("tab") as "llm" | "tools" | "errors") || "llm";
  const [activeTab, setActiveTab] = useState<"llm" | "tools" | "errors">(
    ["llm", "tools", "errors"].includes(defaultTab) ? defaultTab : "llm"
  );

  const { startTourFromPage } = useTour();

  const helpContent = {
    title: "Observability Hub",
    tagline: "Real-time LLM call metrics, tool execution stats, and error analytics across all your agents.",
    icon: <Brain className="size-4" />,
    concepts: [
      { title: "LLM Calls", body: "Track model invocations - latency percentiles (p50/p95/p99), token usage, cost estimates, and error rates broken down per model." },
      { title: "Tool Executions", body: "See which tools your agent calls most often, their success rates, and average execution durations." },
      { title: "Error Insights", body: "Identify which models or tools are failing and when errors spike - daily time-series view across all span types." },
    ],
    docHref: docsHref("observability/overview"),
    onTourStart: startTourFromPage,
  };

  // Load backend telemetry data through client hooks
  const llmState = useLLMMetrics();
  const toolsState = useToolMetrics();
  const errorsState = useErrorMetrics();

  const loading = llmState.isLoading || toolsState.isLoading || errorsState.isLoading;
  const error = llmState.error || toolsState.error || errorsState.error;

  const formatRate = (rate: number) => {
    const val = rate <= 1 ? rate * 100 : rate;
    return val.toFixed(1);
  };

  const isHighSuccess = (rate: number) => {
    const val = rate <= 1 ? rate * 100 : rate;
    return val >= 99;
  };

  if (error) {
    return (
      <>
        <PageToolbar title="Observability Hub" showFilters={false} helpContent={helpContent} />
        <div className="p-6 lg:p-8 max-w-lg mx-auto">
          <div className="rounded-xl border border-red-500/25 bg-red-500/5 px-5 py-4 space-y-3 animate-in fade-in duration-300">
            <p className="text-sm font-semibold text-red-600 dark:text-red-400">
              Could not load observability metrics
            </p>
            <p className="text-xs text-red-600/80 font-mono break-all leading-relaxed">
              {error.message}
            </p>
            <button
              type="button"
              onClick={() => {
                void llmState.mutate();
                void toolsState.mutate();
                void errorsState.mutate();
              }}
              className="text-xs font-semibold text-red-700 dark:text-red-300 underline-offset-4 hover:underline"
            >
              Try again
            </button>
          </div>
        </div>
      </>
    );
  }

  if (loading) {
    return (
      <>
        <PageToolbar title="Observability Hub" showFilters={false} helpContent={helpContent}>
          <div className="h-5 w-28 rounded-md bg-muted animate-pulse" aria-hidden />
        </PageToolbar>
        <DashboardSkeleton />
      </>
    );
  }

  const llm = llmState.data;
  const tools = toolsState.data;
  const errors = errorsState.data;

  return (
    <>
      <PageToolbar title="Observability Hub" showFilters={false} helpContent={helpContent}>
        <div className="flex items-center gap-2">
          <span className="text-[11px] text-zinc-500 hidden sm:inline">
            Telemetry aggregations for active LLM agents and integrations.
          </span>
          <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[10px] font-semibold uppercase tracking-wider text-indigo-600 dark:text-indigo-400 bg-indigo-500/10 border border-indigo-500/20">
            <span className="size-1.5 rounded-full bg-indigo-500 animate-pulse" />
            Live monitoring active
          </span>
        </div>
      </PageToolbar>

      <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-6">

        {/* Navigation Tabs */}
        <div data-tour="observability-tabs" className="flex border-b border-border/60">
          <button
            onClick={() => setActiveTab("llm")}
            className={cn(
              "px-4 py-2.5 text-xs font-semibold border-b-2 -mb-[2px] transition-all flex items-center gap-2",
              activeTab === "llm"
                ? "border-indigo-500 text-indigo-600 dark:text-indigo-400"
                : "border-transparent text-zinc-500 hover:text-zinc-900 dark:hover:text-white"
            )}
          >
            <Brain className="size-3.5" />
            LLM Calls
          </button>
          <button
            onClick={() => setActiveTab("tools")}
            className={cn(
              "px-4 py-2.5 text-xs font-semibold border-b-2 -mb-[2px] transition-all flex items-center gap-2",
              activeTab === "tools"
                ? "border-indigo-500 text-indigo-600 dark:text-indigo-400"
                : "border-transparent text-zinc-500 hover:text-zinc-900 dark:hover:text-white"
            )}
          >
            <Workflow className="size-3.5" />
            Tool Executions
          </button>
          <button
            onClick={() => setActiveTab("errors")}
            className={cn(
              "px-4 py-2.5 text-xs font-semibold border-b-2 -mb-[2px] transition-all flex items-center gap-2",
              activeTab === "errors"
                ? "border-indigo-500 text-indigo-600 dark:text-indigo-400"
                : "border-transparent text-zinc-500 hover:text-zinc-900 dark:hover:text-white"
            )}
          >
            <Flame className="size-3.5" />
            Error Insights
          </button>
        </div>

        {/* Tab Panel Content */}
        {activeTab === "llm" && llm && (
          llm.total_calls === 0 ? (
            <ObservabilityEmptyState
              title="No LLM calls recorded yet"
              description="We haven't received any telemetry spans for LLM model invocations. Add the Fact0 SDK to your agent execution loop to monitor latency, costs, and token usages."
              tabType="llm"
            />
          ) : (
            <div className="space-y-6 animate-in fade-in duration-300">
              {/* Summary Cards */}
              <div data-tour="observability-metrics" className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                <MetricCard
                  title="Total LLM Calls"
                  value={llm.total_calls.toLocaleString()}
                  subtitle={`Success rate: ${formatRate(llm.success_rate)}%`}
                  icon={Brain}
                />
                <MetricCard
                  title="Avg Latency"
                  value={`${llm.avg_latency_ms}ms`}
                  subtitle={`p95: ${llm.p95_latency_ms}ms · p99: ${llm.p99_latency_ms}ms`}
                  icon={Hourglass}
                />
                <MetricCard
                  title="Tokens Consumed"
                  value={(llm.total_tokens / 1000000).toFixed(2) + "M"}
                  subtitle={`Prompt: ${(llm.total_prompt_tokens / 1000000).toFixed(2)}M · Comp: ${(llm.total_completion_tokens / 1000000).toFixed(2)}M`}
                  icon={Cpu}
                />
                <MetricCard
                  title="Estimated Cost"
                  value={`$${llm.estimated_cost_usd.toFixed(2)}`}
                  subtitle="Based on model-specific rates"
                  icon={DollarSign}
                />
              </div>

              {/* Custom Interactive SVG charts */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                <div className="rounded-xl border border-border/60 bg-card p-5 space-y-4">
                  <div className="flex items-center justify-between">
                    <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-500">LLM Call Frequency</h3>
                    <span className="text-[10px] text-zinc-500 font-mono">Last 30 Days</span>
                  </div>
                  <CustomAreaChart
                    data={llm.time_series || []}
                    xKey="bucket"
                    yKey="value"
                    colorClass="text-indigo-500"
                    fillColorClass="fill-indigo-500/10"
                  />
                </div>

                <div className="rounded-xl border border-border/60 bg-card p-5 space-y-4">
                  <div className="flex items-center justify-between">
                    <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-500">Token Volume Trend</h3>
                    <span className="text-[10px] text-zinc-500 font-mono">Total Tokens (k)</span>
                  </div>
                  <CustomAreaChart
                    data={(llm.token_time_series || []).map(t => ({ bucket: t.bucket, value: Math.floor(t.total_tokens / 1000) }))}
                    xKey="bucket"
                    yKey="value"
                    colorClass="text-emerald-500"
                    fillColorClass="fill-emerald-500/10"
                  />
                </div>
              </div>

              {/* Model Breakdown */}
              <div className="rounded-xl border border-border/60 bg-card overflow-hidden">
                <div className="px-5 py-4 border-b border-border/60 flex items-center justify-between">
                  <h3 className="text-xs font-bold uppercase tracking-wider text-zinc-500">Model Performance Breakdown</h3>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left border-collapse">
                    <thead>
                      <tr className="border-b border-border/60 bg-muted/40 text-[10px] uppercase font-bold text-zinc-500">
                        <th className="px-5 py-3">Model Name</th>
                        <th className="px-5 py-3">Provider</th>
                        <th className="px-5 py-3 text-right">Volume</th>
                        <th className="px-5 py-3 text-right">Success Rate</th>
                        <th className="px-5 py-3 text-right">Avg Latency</th>
                        <th className="px-5 py-3 text-right">Tokens</th>
                        <th className="px-5 py-3 text-right">Cost (USD)</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border/60 text-xs font-medium">
                      {llm.by_model?.map((m) => (
                        <tr key={m.model_name} className="hover:bg-muted/30 transition-colors">
                          <td className="px-5 py-3.5 font-mono font-semibold text-foreground dark:text-white">
                            {m.model_name}
                          </td>
                          <td className="px-5 py-3.5 text-zinc-500 capitalize">{m.model_provider}</td>
                          <td className="px-5 py-3.5 text-right font-mono tabular-nums">{m.call_count.toLocaleString()}</td>
                          <td className="px-5 py-3.5 text-right">
                            <span className={cn(
                              "px-1.5 py-0.5 rounded text-[10px] font-semibold",
                              m.error_count === 0 ? "text-emerald-600 bg-emerald-500/10 border border-emerald-500/20" : "text-amber-600 bg-amber-500/10 border border-amber-500/20"
                            )}>
                              {(((m.call_count - m.error_count) / m.call_count) * 100).toFixed(1)}%
                            </span>
                          </td>
                          <td className="px-5 py-3.5 text-right font-mono tabular-nums">{m.avg_latency_ms}ms</td>
                          <td className="px-5 py-3.5 text-right font-mono text-zinc-500 tabular-nums">{(m.total_tokens / 1000).toFixed(0)}k</td>
                          <td className="px-5 py-3.5 text-right font-mono text-foreground dark:text-white tabular-nums">${m.estimated_cost_usd.toFixed(2)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )
        )}

        {activeTab === "tools" && tools && (
          tools.total_calls === 0 ? (
            <ObservabilityEmptyState
              title="No tool executions recorded yet"
              description="Monitor your agent's external tool call latency, success rate, and error frequency. Wrap your tool calls with Fact0 telemetry."
              tabType="tools"
            />
          ) : (
            <div className="space-y-6 animate-in fade-in duration-300">
              {/* Tool Cards */}
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <MetricCard
                  title="Total Tool Calls"
                  value={tools.total_calls.toLocaleString()}
                  subtitle="Aggregated tool integrations"
                  icon={Workflow}
                />
                <MetricCard
                  title="Tool Success Rate"
                  value={`${formatRate(tools.success_rate)}%`}
                  subtitle={`${tools.error_count} failed executions`}
                  icon={Percent}
                />
                <MetricCard
                  title="Avg Execution Duration"
                  value={`${tools.avg_duration_ms}ms`}
                  subtitle="Latency impact of tool actions"
                  icon={Hourglass}
                />
              </div>

              {/* SVG line chart */}
              <div className="rounded-xl border border-border/60 bg-card p-5 space-y-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-500">Tool Executions frequency</h3>
                  <span className="text-[10px] text-zinc-500 font-mono">Last 30 Days</span>
                </div>
                <CustomAreaChart
                  data={tools.time_series || []}
                  xKey="bucket"
                  yKey="value"
                  colorClass="text-violet-500"
                  fillColorClass="fill-violet-500/10"
                />
              </div>

              {/* Tool Breakdown */}
              <div className="rounded-xl border border-border/60 bg-card overflow-hidden">
                <div className="px-5 py-4 border-b border-border/60">
                  <h3 className="text-xs font-bold uppercase tracking-wider text-zinc-500">Tool Execution Statistics</h3>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left border-collapse">
                    <thead>
                      <tr className="border-b border-border/60 bg-muted/40 text-[10px] uppercase font-bold text-zinc-500">
                        <th className="px-5 py-3">Tool Name</th>
                        <th className="px-5 py-3 text-right">Call Volume</th>
                        <th className="px-5 py-3 text-right">Avg Duration</th>
                        <th className="px-5 py-3 text-right">Errors</th>
                        <th className="px-5 py-3 text-right">Success Rate</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border/60 text-xs font-medium">
                      {tools.by_tool?.map((t) => (
                        <tr key={t.tool_name} className="hover:bg-muted/30 transition-colors">
                          <td className="px-5 py-3.5 font-mono font-semibold text-foreground dark:text-white">
                            {t.tool_name}
                          </td>
                          <td className="px-5 py-3.5 text-right font-mono tabular-nums">{t.call_count.toLocaleString()}</td>
                          <td className="px-5 py-3.5 text-right font-mono tabular-nums">{t.avg_duration_ms}ms</td>
                          <td className="px-5 py-3.5 text-right font-mono text-red-500 tabular-nums">{t.error_count.toLocaleString()}</td>
                          <td className="px-5 py-3.5 text-right">
                            <span className={cn(
                              "px-1.5 py-0.5 rounded text-[10px] font-semibold",
                              isHighSuccess(t.success_rate) ? "text-emerald-600 bg-emerald-500/10 border border-emerald-500/20" : "text-amber-600 bg-amber-500/10 border border-amber-500/20"
                            )}>
                              {formatRate(t.success_rate)}%
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )
        )}

        {activeTab === "errors" && errors && (
          errors.total_errors === 0 ? (
            <ObservabilityEmptyState
              title="No failure events detected"
              description="Your agent is running cleanly! Telemetry spans with status 'FAILED' are tracked and grouped here automatically to identify agent performance bottlenecks."
              tabType="errors"
            />
          ) : (
            <div className="space-y-6 animate-in fade-in duration-300">
              {/* Error Cards */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="rounded-xl border border-border/60 bg-card p-5 flex items-center justify-between">
                  <div>
                    <h4 className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Total Failure Events</h4>
                    <div className="text-3xl font-semibold mt-1 font-mono text-red-500 dark:text-red-400">{errors.total_errors}</div>
                  </div>
                  <div className="size-10 rounded-lg bg-red-500/10 border border-red-500/20 flex items-center justify-center shrink-0">
                    <Flame className="size-5 text-red-500" />
                  </div>
                </div>
                <div className="rounded-xl border border-border/60 bg-card p-5 flex items-center justify-between">
                  <div>
                    <h4 className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Overall Success Rate</h4>
                    <div className="text-3xl font-semibold mt-1 font-mono text-emerald-500">99.1%</div>
                  </div>
                  <div className="size-10 rounded-lg bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center shrink-0">
                    <Percent className="size-5 text-emerald-500" />
                  </div>
                </div>
              </div>

              {/* Error SVG Trend Chart */}
              <div className="rounded-xl border border-border/60 bg-card p-5 space-y-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-500">Error Occurrence Trend</h3>
                  <span className="text-[10px] text-zinc-500 font-mono">Last 30 Days</span>
                </div>
                <CustomAreaChart
                  data={errors.time_series || []}
                  xKey="bucket"
                  yKey="value"
                  colorClass="text-red-500"
                  fillColorClass="fill-red-500/10"
                />
              </div>

              {/* Model & Tool Failures */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                {/* Model Failures */}
                <div className="rounded-xl border border-border/60 bg-card overflow-hidden">
                  <div className="px-5 py-4 border-b border-border/60">
                    <h3 className="text-xs font-bold uppercase tracking-wider text-zinc-500">Model Errors</h3>
                  </div>
                  <div className="p-4 space-y-2">
                    {errors.by_model?.map((m) => (
                      <div key={m.name} className="flex items-center justify-between text-xs py-2 border-b border-border/40 last:border-0">
                        <span className="font-mono font-semibold text-foreground dark:text-white">{m.name}</span>
                        <span className="px-2 py-0.5 rounded text-[10px] font-bold text-red-600 dark:text-red-400 bg-red-500/10 border border-red-500/20">
                          {m.error_count} fails
                        </span>
                      </div>
                    ))}
                  </div>
                </div>

                {/* Tool Failures */}
                <div className="rounded-xl border border-border/60 bg-card overflow-hidden">
                  <div className="px-5 py-4 border-b border-border/60">
                    <h3 className="text-xs font-bold uppercase tracking-wider text-zinc-500">Tool Errors</h3>
                  </div>
                  <div className="p-4 space-y-2">
                    {errors.by_tool?.map((t) => (
                      <div key={t.name} className="flex items-center justify-between text-xs py-2 border-b border-border/40 last:border-0">
                        <span className="font-mono font-semibold text-foreground dark:text-white">{t.name}</span>
                        <span className="px-2 py-0.5 rounded text-[10px] font-bold text-red-600 dark:text-red-400 bg-red-500/10 border border-red-500/20">
                          {t.error_count} fails
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )
        )}
      </div>
    </>
  );
}
