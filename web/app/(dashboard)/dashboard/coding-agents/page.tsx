"use client";

import {
  AlertCircle,
  Bot,
  ChevronRight,
  Clock,
  FileCode2,
  GitBranch,
  MessageSquare,
  RefreshCw,
  TerminalSquare,
  Wrench,
} from "lucide-react";
import Link from "next/link";
import { Suspense, useState } from "react";
import { useTelemetryClient } from "@/lib/api";
import useSWR from "swr";

import { PageGetStarted } from "@/components/dashboard/page-get-started";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { RetentionBanner } from "@/components/dashboard/retention-banner";
import {
  type ClaudeCodeSession,
  type ClaudeCodeSessionListResponse,
  useClaudeCodeClient,
} from "@/lib/claude-code-api";
import { useDashboardHref } from "@/lib/demo/demo-context";
import { docsHref } from "@/lib/docs-origin";
import { useLiveAuditStream } from "@/lib/use-live-events";
import { cn, formatDuration, relativeTime } from "@/lib/utils";
import { useEffect } from "react";

export default function CodingAgentsPage() {
  return (
    <Suspense fallback={<CodingAgentsFallback />}>
      <CodingAgentsContent />
    </Suspense>
  );
}

function CodingAgentsFallback() {
  return (
    <>
      <PageToolbar title="Coding Agents" showFilters={false} />
      <div className="p-6 lg:p-8">
        <SkeletonList />
      </div>
    </>
  );
}

function CodingAgentsContent() {
  const client = useClaudeCodeClient();
  const { ready, orgId } = useTelemetryClient();
  const [page, setPage] = useState(1);

  const helpContent = {
    title: "Coding Agents",
    tagline:
      "Every Claude Code session on your machines — what was prompted, which files were touched, which commands ran, and what was captured.",
    icon: <Bot className="size-4" />,
    concepts: [
      {
        title: "Session",
        body: "One Claude Code conversation, from launch to exit. Sessions are captured automatically by the fact0-claude-code plugin.",
      },
      {
        title: "Capture modes",
        body: "Raw capture records tool inputs, outputs and assistant text. Metadata/hash modes reduce content; unavailable or partial transcripts are labelled.",
      },
    ],
    docHref: docsHref("integrations/claude-code"),
  };

  const { data, error, isLoading, mutate } = useSWR<
    ClaudeCodeSessionListResponse,
    Error
  >(
    ready ? ["claude-code-sessions", orgId, page] : null,
    () => client.listSessions({ page, page_size: 50 }),
    { refreshInterval: 5_000, revalidateOnFocus: true },
  );

  // Refresh the list the moment any coding-agent event streams in.
  const live = useLiveAuditStream({
    bufferSize: 10,
    filter: (e) => e.action.startsWith("claude_code."),
  });
  useEffect(() => {
    if (live.lastReceivedAt) void mutate();
  }, [live.lastReceivedAt, mutate]);

  const sessions = data?.sessions ?? [];
  const total = data?.total ?? 0;
  const loading = isLoading;

  return (
    <>
      <PageToolbar
        title="Coding Agents"
        showFilters={false}
        helpContent={helpContent}
      >
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-zinc-500 tabular-nums">
            {total.toLocaleString()} {total === 1 ? "session" : "sessions"}
          </span>
          <div className="flex items-center gap-2">
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
        </div>
      </PageToolbar>

      <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8 space-y-4">
        <RetentionBanner />
        {error && (
          <ErrorBanner message={error.message} onRetry={() => mutate()} />
        )}

        {!loading && sessions.length === 0 && !error && (
          <PageGetStarted
            icon={<Bot className="size-5 text-zinc-400" />}
            title="No coding sessions yet"
            description="Install the fact0-claude-code plugin and every Claude Code session streams here automatically — prompts, file edits, commands, and capture details, hash-chained and queryable."
            docLinks={[
              {
                label: "Install the plugin",
                href: docsHref("integrations/claude-code"),
              },
              {
                label: "Privacy & capture modes",
                href: docsHref("integrations/claude-code#capture-modes"),
              },
            ]}
          />
        )}

        {loading && sessions.length === 0 && <SkeletonList />}

        {sessions.length > 0 && (
          <ul className="rounded-xl border border-border/60 bg-card divide-y divide-border/40 overflow-hidden">
            {sessions.map((s) => (
              <SessionRow key={s.session_id} session={s} />
            ))}
          </ul>
        )}
        <div className="flex items-center gap-3 text-sm">
          <button
            className="rounded border px-3 py-1 disabled:opacity-40"
            disabled={page === 1}
            onClick={() => setPage((p) => p - 1)}
          >
            Previous
          </button>
          <span>
            Page {page} · {total} sessions
          </span>
          <button
            className="rounded border px-3 py-1 disabled:opacity-40"
            disabled={page * 50 >= total}
            onClick={() => setPage((p) => p + 1)}
          >
            Next
          </button>
        </div>
      </div>
    </>
  );
}

function repoName(cwd: string): string {
  if (!cwd) return "unknown repo";
  const parts = cwd.split("/").filter(Boolean);
  return parts[parts.length - 1] || cwd;
}

function SessionRow({ session: s }: { session: ClaudeCodeSession }) {
  const base = useDashboardHref("/coding-agents");
  const active = s.status === "active";

  return (
    <li>
      <Link
        href={`${base}/${encodeURIComponent(s.session_id)}`}
        className="flex items-center gap-4 px-5 py-3 hover:bg-muted/30 transition-colors group"
      >
        <span
          className={cn(
            "size-2 rounded-full shrink-0",
            active && "bg-emerald-500 animate-pulse",
            s.status === "idle" && "bg-amber-400",
            s.status === "completed" && "bg-zinc-400 dark:bg-zinc-600",
          )}
        />

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3 min-w-0">
            <span className="text-sm font-medium text-foreground dark:text-white truncate">
              {repoName(s.cwd)}
            </span>
            {s.git_branch && (
              <span className="inline-flex items-center gap-1 shrink-0 rounded bg-indigo-500/10 px-1.5 py-0.5 text-[10px] font-mono text-indigo-500">
                <GitBranch className="size-2.5" />
                {s.git_branch}
              </span>
            )}
            <span className="text-[11px] font-mono text-zinc-400 truncate">
              {s.cwd}
            </span>
          </div>
          <div className="flex items-center gap-4 mt-1 text-[11px] text-zinc-500">
            <Stat icon={MessageSquare} value={s.prompts} label="prompts" />
            <Stat icon={Wrench} value={s.tool_calls} label="tools" />
            <Stat icon={FileCode2} value={s.files_touched} label="files" />
            <Stat icon={TerminalSquare} value={s.commands} label="cmds" />
            <span className="inline-flex items-center gap-1 shrink-0">
              <Clock className="size-3" />
              <span className="font-mono tabular-nums">
                {formatDuration(s.duration_s * 1000)}
              </span>
            </span>
            <span className="shrink-0 uppercase tracking-wide tabular-nums">
              {relativeTime(s.last_activity_at)}
            </span>
          </div>
        </div>

        {s.failures > 0 && (
          <span className="inline-flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 shrink-0">
            <AlertCircle className="size-3" />
            {s.failures} failed
          </span>
        )}
        {s.cost_usd > 0 && (
          <span className="text-[11px] font-mono tabular-nums text-zinc-500 shrink-0">
            ${s.cost_usd.toFixed(3)}
          </span>
        )}
        <span
          className={cn(
            "text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded shrink-0",
            active &&
              "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
            s.status === "idle" &&
              "bg-amber-500/10 text-amber-600 dark:text-amber-400",
            s.status === "completed" && "bg-zinc-500/10 text-zinc-500",
          )}
        >
          {s.status}
        </span>
        <ChevronRight className="size-3.5 text-zinc-300 dark:text-zinc-700 group-hover:text-indigo-500 group-hover:translate-x-0.5 transition-all shrink-0" />
      </Link>
    </li>
  );
}

function Stat({
  icon: Icon,
  value,
  label,
}: {
  icon: typeof Clock;
  value: number;
  label: string;
}) {
  return (
    <span className="inline-flex items-center gap-1 shrink-0">
      <Icon className="size-3" />
      <span className="font-mono tabular-nums">{value}</span>
      <span className="text-zinc-400">{label}</span>
    </span>
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

function ErrorBanner({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-red-500/30 bg-red-500/5 px-4 py-3">
      <AlertCircle className="size-4 text-red-500 shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="text-xs font-medium text-red-600 dark:text-red-400">
          Failed to load coding sessions
        </p>
        <p className="text-[11px] text-red-500/70 font-mono truncate">
          {message}
        </p>
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
