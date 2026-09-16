"use client";

import {
  Brain,
  ChevronRight,
  DollarSign,
  Layers,
  MessageSquare,
  Workflow
} from "lucide-react";
import { useState } from "react";

import { DashboardSkeleton } from "@/components/dashboard/dashboard-skeleton";
import { PageGetStarted } from "@/components/dashboard/page-get-started";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { docsHref } from "@/lib/docs-origin";
import { useListSessions, useSessionDetail } from "@/lib/use-analytics";
import { useTour } from "@/lib/use-tour";
import { cn } from "@/lib/utils";

export default function SessionsPage() {
  const [selectedSessionID, setSelectedSessionID] = useState<string>("sess_01HZF1A1A1");
  const limit = 20;
  const offset = 0;

  const { startTourFromPage } = useTour();

  const helpContent = {
    title: "Conversations",
    tagline: "Multi-turn agent interactions grouped by session ID - see the full conversation arc and cumulative cost.",
    icon: <MessageSquare className="size-4" />,
    concepts: [
      { title: "Session", body: "A series of related executions grouped by the session_id you supply in your SDK span metadata. Represents one complete user conversation." },
      { title: "Turn", body: "Each individual execution within a session. Turn sequence is derived from turn_sequence metadata on the execution." },
      { title: "Cost & Tokens", body: "Aggregated across all turns in the session, rolling up individual LLM call token counts and cost estimates." },
    ],
    docHref: docsHref("observability/overview"),
    onTourStart: startTourFromPage,
  };

  // Fetch list of sessions
  const sessionsState = useListSessions(limit, offset);
  // Fetch details of selected session
  const detailState = useSessionDetail(selectedSessionID);

  const loading = sessionsState.isLoading;
  const sessions = sessionsState.data || [];
  const detail = detailState.data;

  return (
    <>
      <PageToolbar title="Conversations" showFilters={false} helpContent={helpContent}>
        <div className="flex items-center gap-2">
          <span className="text-[11px] text-zinc-500 hidden sm:inline">
            Analyze multi-turn conversation sessions and agent execution workflows.
          </span>
        </div>
      </PageToolbar>

      <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8">
        {loading ? (
          <DashboardSkeleton />
        ) : (
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 h-[calc(100vh-200px)] min-h-[500px]">

            {/* Left Pane: Sessions List (4 cols) */}
            <div data-tour="sessions-list" className="lg:col-span-4 border border-border/60 bg-card rounded-xl flex flex-col overflow-hidden h-full">
              <div className="px-4 py-3 border-b border-border/60 bg-muted/20 flex items-center justify-between">
                <span className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Active Sessions</span>
                <span className="text-[10px] font-semibold font-mono text-zinc-400">{sessions.length} sessions</span>
              </div>

              <div className="flex-1 overflow-y-auto divide-y divide-border/60">
                {sessions.length === 0 ? (
                  <PageGetStarted
                    icon={<MessageSquare className="size-5 text-zinc-400" />}
                    title="No sessions yet"
                    description="Sessions group related LLM calls into a single user interaction. Instrument your agent with the Fact0 SDK to capture sessions automatically."
                    docLinks={[
                      { label: "Observability guide", href: docsHref("observability/overview") },
                      { label: "SDK overview", href: docsHref("sdk/overview") },
                    ]}
                    onTourStart={startTourFromPage}
                    className="border-0 rounded-none"
                  />
                ) : sessions.map((s) => {
                  const isSelected = s.session_id === selectedSessionID;
                  return (
                    <button
                      key={s.session_id}
                      onClick={() => setSelectedSessionID(s.session_id)}
                      className={cn(
                        "w-full text-left p-4 hover:bg-muted/40 transition-colors flex items-start justify-between relative",
                        isSelected && "bg-muted/50 border-r-2 border-indigo-500"
                      )}
                    >
                      <div className="space-y-1 min-w-0 pr-2">
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-semibold text-foreground dark:text-white truncate">
                            {s.agent_name || "Unknown Agent"}
                          </span>
                          <span className={cn(
                            "px-1.5 py-0.5 rounded text-[8px] font-bold uppercase tracking-wider",
                            s.status === "active" ? "text-emerald-600 bg-emerald-500/10" : "text-zinc-500 bg-zinc-500/10"
                          )}>
                            {s.status}
                          </span>
                        </div>
                        <div className="text-[10px] text-zinc-500 font-mono truncate">{s.session_id}</div>
                        <div className="flex items-center gap-3 text-[10px] text-zinc-400 font-medium">
                          <span className="flex items-center gap-1">
                            <Layers className="size-3" />
                            {s.turn_count} turns
                          </span>
                          <span className="flex items-center gap-1 font-mono">
                            <DollarSign className="size-2.5" />
                            {s.total_cost_usd.toFixed(3)}
                          </span>
                        </div>
                      </div>
                      <ChevronRight className={cn("size-4 text-zinc-400 shrink-0 mt-0.5", isSelected && "text-indigo-500")} />
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Right Pane: Session Detail (8 cols) */}
            <div data-tour="session-detail" className="lg:col-span-8 border border-border/60 bg-card rounded-xl flex flex-col overflow-hidden h-full">
              {detailState.isLoading ? (
                <div className="flex-1 flex flex-col items-center justify-center space-y-3">
                  <div className="h-6 w-6 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
                  <span className="text-xs text-zinc-500">Loading session trace...</span>
                </div>
              ) : detail ? (
                <div className="flex-1 flex flex-col overflow-hidden">

                  {/* Detail Header */}
                  <div className="px-6 py-4 border-b border-border/60 bg-muted/20 flex flex-wrap items-center justify-between gap-4">
                    <div>
                      <div className="flex items-center gap-2">
                        <h3 className="text-sm font-bold text-foreground dark:text-white">
                          {detail.session.agent_name}
                        </h3>
                        <span className="text-[10px] font-mono text-zinc-500">{detail.session.session_id}</span>
                      </div>
                      <div className="text-[10px] text-zinc-500 mt-0.5 flex items-center gap-2">
                        <span>Started {new Date(detail.session.started_at).toLocaleTimeString()}</span>
                        <span>·</span>
                        <span>Agent ID: {detail.session.agent_id}</span>
                      </div>
                    </div>

                    <div className="flex items-center gap-4 bg-background border border-border/60 px-3 py-1.5 rounded-lg text-xs font-medium text-zinc-600 dark:text-zinc-300 font-mono">
                      <div className="text-center">
                        <div className="text-[9px] uppercase tracking-wider text-zinc-500 font-sans font-bold">Turns</div>
                        <div className="mt-0.5 tabular-nums text-foreground dark:text-white">{detail.session.turn_count}</div>
                      </div>
                      <div className="h-4 w-px bg-border/60" />
                      <div className="text-center">
                        <div className="text-[9px] uppercase tracking-wider text-zinc-500 font-sans font-bold">Tokens</div>
                        <div className="mt-0.5 tabular-nums text-foreground dark:text-white">{detail.session.total_tokens.toLocaleString()}</div>
                      </div>
                      <div className="h-4 w-px bg-border/60" />
                      <div className="text-center">
                        <div className="text-[9px] uppercase tracking-wider text-zinc-500 font-sans font-bold">Cost</div>
                        <div className="mt-0.5 tabular-nums text-foreground dark:text-white">${detail.session.total_cost_usd.toFixed(3)}</div>
                      </div>
                    </div>
                  </div>

                  {/* Turns Timeline */}
                  <div className="flex-1 overflow-y-auto p-6 space-y-6">
                    <div className="relative border-l border-zinc-200 dark:border-zinc-800 ml-3 space-y-6">
                      {detail.turns.map((turn) => (
                        <div key={turn.execution_id} className="relative pl-6 group">
                          {/* Timeline dot */}
                          <div className="absolute -left-[5px] top-1.5 size-2.5 rounded-full bg-indigo-500 ring-4 ring-background transition-transform group-hover:scale-125" />

                          <div className="rounded-xl border border-border/60 bg-card hover:border-indigo-500/30 transition-colors p-4 space-y-3">
                            <div className="flex items-center justify-between text-xs">
                              <div className="flex items-center gap-2">
                                <span className="font-semibold text-foreground dark:text-white">Turn {turn.sequence}</span>
                                <span className="text-[10px] font-mono text-zinc-500">{turn.execution_id}</span>
                              </div>
                              <div className="flex items-center gap-3 text-zinc-500 font-mono text-[10px]">
                                <span>{turn.duration_ms}ms</span>
                                <span>·</span>
                                <span>{turn.total_tokens.toLocaleString()} tokens</span>
                                <span>·</span>
                                <span>${turn.total_cost_usd.toFixed(3)}</span>
                              </div>
                            </div>

                            {/* Nested LLM Calls */}
                            {turn.llm_calls && turn.llm_calls.length > 0 && (
                              <div className="space-y-1.5">
                                <div className="text-[9px] font-bold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5">
                                  <Brain className="size-3 text-indigo-500" />
                                  LLM Interactions
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                                  {turn.llm_calls.map((llm, li) => (
                                    <div key={li} className="rounded-lg bg-muted/40 border border-border/40 p-2.5 flex items-center justify-between text-xs">
                                      <div className="min-w-0 pr-2">
                                        <div className="font-mono font-semibold text-foreground dark:text-white truncate">{llm.model_name}</div>
                                        <div className="text-[10px] text-zinc-500 font-mono mt-0.5">{llm.span_id}</div>
                                      </div>
                                      <div className="text-right shrink-0">
                                        <div className="font-mono text-zinc-600 dark:text-zinc-400">{llm.latency_ms}ms</div>
                                        <div className="font-mono text-[10px] text-zinc-500 mt-0.5">{llm.total_tokens} tokens</div>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            )}

                            {/* Nested Tool Calls */}
                            {turn.tool_calls && turn.tool_calls.length > 0 && (
                              <div className="space-y-1.5 pt-1">
                                <div className="text-[9px] font-bold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5">
                                  <Workflow className="size-3 text-violet-500" />
                                  Tool Executions
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                                  {turn.tool_calls.map((tool, ti) => (
                                    <div key={ti} className="rounded-lg bg-muted/40 border border-border/40 p-2.5 flex items-center justify-between text-xs">
                                      <div className="min-w-0 pr-2">
                                        <div className="font-mono font-semibold text-foreground dark:text-white truncate">{tool.tool_name}</div>
                                        <div className="text-[10px] text-zinc-500 font-mono mt-0.5">{tool.span_id}</div>
                                      </div>
                                      <div className="text-right shrink-0">
                                        <div className="font-mono text-zinc-600 dark:text-zinc-400">{tool.duration_ms}ms</div>
                                        <span className="inline-block mt-1 px-1 rounded text-[8px] font-bold uppercase text-emerald-600 bg-emerald-500/10">
                                          {tool.status.toLowerCase()}
                                        </span>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            )}

                          </div>
                        </div>
                      ))}
                    </div>
                  </div>

                </div>
              ) : (
                <div className="flex-1 flex flex-col items-center justify-center text-zinc-500 text-xs">
                  <MessageSquare className="size-8 text-zinc-400 mb-2" />
                  Select a session to view details
                </div>
              )}
            </div>

          </div>
        )}
      </div>
    </>
  );
}
