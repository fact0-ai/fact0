"use client";

import { useEffect, useState } from "react";
import useSWR from "swr";
import { Download, Search } from "lucide-react";
import { useAuditClient } from "@/lib/audit-api";
import { useBootstrap } from "@/lib/use-me";
import type {
  ActorType,
  AuditFilter,
  Outcome,
  VerifyResult,
} from "@/lib/audit-types";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { CapturedPayload } from "@/components/inspector/captured-payload";
import { useLiveAuditStream } from "@/lib/use-live-events";

const inputClass = "w-full rounded-lg border bg-background px-3 py-2 text-sm";
const buttonClass =
  "inline-flex items-center justify-center gap-2 rounded-lg border px-3 py-2 text-sm disabled:opacity-40";
type ExportFormat = "pdf" | "zip";

export default function AuditPage() {
  const client = useAuditClient();
  const bootstrap = useBootstrap();
  const [filter, setFilter] = useState<AuditFilter>({ page: 1, page_size: 50 });
  const [fromInput, setFromInput] = useState("");
  const [toInput, setToInput] = useState("");
  const [verification, setVerification] = useState<{
    scope: string;
    result: VerifyResult;
  } | null>(null);
  const [verifyError, setVerifyError] = useState("");
  const [verifying, setVerifying] = useState(false);
  const [exporting, setExporting] = useState<ExportFormat | null>(null);
  const [exportError, setExportError] = useState("");
  const [exportNotice, setExportNotice] = useState("");
  const invalidRange = !!(filter.from && filter.to && filter.from > filter.to);
  const scope = `${filter.from ?? ""}|${filter.to ?? ""}`;
  const ready = !!bootstrap.data && !invalidRange;
  const { data, error, isLoading, mutate } = useSWR(
    ready ? ["audit", bootstrap.data!.tenant.id, filter] : null,
    () => client.listEvents(filter),
  );
  const live = useLiveAuditStream();
  useEffect(() => {
    if (live.lastReceivedAt) void mutate();
  }, [live.lastReceivedAt, mutate]);

  const updateFilter = (patch: Partial<AuditFilter>) =>
    setFilter((current) => ({ ...current, ...patch, page: 1 }));
  const setDate = (field: "from" | "to", value: string) => {
    if (field === "from") setFromInput(value);
    else setToInput(value);
    const date = value ? new Date(value) : null;
    updateFilter({
      [field]:
        date && Number.isFinite(date.getTime())
          ? date.toISOString()
          : undefined,
    });
    setExportNotice("");
  };
  const download = async (format: ExportFormat) => {
    if (!ready || exporting) return;
    setExporting(format);
    setExportError("");
    setExportNotice("");
    try {
      const blob =
        format === "pdf"
          ? await client.downloadPDF(filter.from, filter.to)
          : await client.downloadEvidencePack(filter.from, filter.to);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `fact0-audit-${new Date().toISOString().slice(0, 10)}.${format}`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      setExportNotice(
        `${format === "pdf" ? "PDF" : "Evidence ZIP"} download started.`,
      );
    } catch (failure) {
      setExportError(
        failure instanceof Error
          ? failure.message
          : "Could not prepare the export. Try again.",
      );
    } finally {
      setExporting(null);
    }
  };
  const loading = (!bootstrap.data && !bootstrap.error) || isLoading;
  const page = filter.page ?? 1;
  const pageSize = filter.page_size ?? 50;
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const checked = verification?.scope === scope ? verification.result : null;

  return (
    <>
      <PageToolbar title="Audit logs" showFilters={false} />
      <div className="p-6 lg:p-8 space-y-5">
        <p className="text-sm text-muted-foreground">
          Recorded activity and hash-chain integrity. Verification detects
          changes in stored records; it does not prove every action was
          captured.
        </p>
        <section
          aria-label="Search audit events"
          className="rounded-xl border bg-card p-4 space-y-4"
        >
          <h2 className="flex items-center gap-2 text-sm font-medium">
            <Search className="size-4" /> Search recorded actions
          </h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <label className="space-y-1 text-xs text-muted-foreground">
              Action or prefix
              <input
                aria-label="Filter action"
                className={inputClass}
                placeholder="e.g. claude_code.*"
                value={filter.action ?? ""}
                onChange={(e) => updateFilter({ action: e.target.value })}
              />
            </label>
            <label className="space-y-1 text-xs text-muted-foreground">
              Actor ID
              <input
                aria-label="Filter actor"
                className={inputClass}
                placeholder="Exact actor ID"
                value={filter.actor_id ?? ""}
                onChange={(e) => updateFilter({ actor_id: e.target.value })}
              />
            </label>
            <label className="space-y-1 text-xs text-muted-foreground">
              Resource ID
              <input
                aria-label="Filter resource"
                className={inputClass}
                placeholder="Exact resource ID or file path"
                value={filter.resource_id ?? ""}
                onChange={(e) => updateFilter({ resource_id: e.target.value })}
              />
            </label>
            <label className="space-y-1 text-xs text-muted-foreground">
              Session ID
              <input
                aria-label="Filter session"
                className={inputClass}
                placeholder="Exact Claude session ID"
                value={filter.session_id ?? ""}
                onChange={(e) => updateFilter({ session_id: e.target.value })}
              />
            </label>
            <label className="space-y-1 text-xs text-muted-foreground">
              Actor type
              <select
                className={inputClass}
                value={filter.actor_type ?? ""}
                onChange={(e) =>
                  updateFilter({ actor_type: e.target.value as ActorType | "" })
                }
              >
                <option value="">All actors</option>
                <option value="agent">Agent</option>
                <option value="human">Human</option>
                <option value="system">System</option>
              </select>
            </label>
            <label className="space-y-1 text-xs text-muted-foreground">
              Outcome
              <select
                className={inputClass}
                value={filter.outcome ?? ""}
                onChange={(e) =>
                  updateFilter({ outcome: e.target.value as Outcome | "" })
                }
              >
                <option value="">All outcomes</option>
                <option value="success">Success</option>
                <option value="failure">Failure</option>
                <option value="error">Error</option>
              </select>
            </label>
          </div>
          <p className="text-xs text-muted-foreground">
            Action matches exactly, or use a trailing * to match a prefix.
            Filters query all stored events in this workspace.
          </p>
          <div className="flex flex-wrap items-end gap-3 border-t pt-4">
            <label className="space-y-1 text-xs text-muted-foreground">
              From (local time)
              <input
                type="datetime-local"
                step="1"
                className={inputClass}
                value={fromInput}
                onChange={(e) => setDate("from", e.target.value)}
              />
            </label>
            <label className="space-y-1 text-xs text-muted-foreground">
              To (local time)
              <input
                type="datetime-local"
                step="1"
                className={inputClass}
                value={toInput}
                onChange={(e) => setDate("to", e.target.value)}
              />
            </label>
            <button
              className={buttonClass}
              onClick={() => {
                setFilter({ page: 1, page_size: pageSize });
                setFromInput("");
                setToInput("");
                setExportNotice("");
              }}
            >
              Clear filters
            </button>
          </div>
          {invalidRange && (
            <p role="alert" className="text-xs text-red-600">
              From must be earlier than or equal to To.
            </p>
          )}
        </section>
        <section
          aria-label="Verification and exports"
          className="rounded-xl border bg-card p-4 space-y-3"
        >
          <div className="flex flex-wrap gap-3">
            <button
              className={buttonClass}
              disabled={!ready || verifying}
              onClick={async () => {
                setVerifying(true);
                setVerifyError("");
                setVerification(null);
                try {
                  setVerification({
                    scope,
                    result: await client.verifyDeep(filter.from, filter.to),
                  });
                } catch (failure) {
                  setVerifyError(
                    failure instanceof Error
                      ? failure.message
                      : "Verification failed. Try again.",
                  );
                } finally {
                  setVerifying(false);
                }
              }}
            >
              {verifying ? "Verifying…" : "Verify chain"}
            </button>
            <button
              className={buttonClass}
              disabled={!ready || exporting !== null}
              onClick={() => void download("pdf")}
            >
              <Download className="size-4" />
              {exporting === "pdf" ? "Preparing PDF…" : "Download PDF"}
            </button>
            <button
              className={buttonClass}
              disabled={!ready || exporting !== null}
              onClick={() => void download("zip")}
            >
              <Download className="size-4" />
              {exporting === "zip"
                ? "Preparing evidence…"
                : "Download evidence ZIP"}
            </button>
          </div>
          <p className="text-xs text-muted-foreground">
            Verification and signed exports include all actors, actions and
            outcomes in the selected time range, across every page. Leave dates
            empty for the complete recorded history. The evidence ZIP includes
            verification data and signatures.
          </p>
          {checked && (
            <div role="status" className="rounded-lg border p-3 text-sm">
              <p>
                {checked.valid ? "Chain intact" : "Chain broken"} ·{" "}
                {checked.events_checked} records checked
              </p>
              {checked.reason && (
                <p className="mt-1 text-xs">{checked.reason}</p>
              )}
              {checked.first_broken_event_id && (
                <p className="mt-1 font-mono text-xs break-all">
                  First broken event: {checked.first_broken_event_id}
                </p>
              )}
              <CapturedPayload label="Verification report" value={checked} />
            </div>
          )}
          {verifyError && (
            <p role="alert" className="text-sm text-red-600">
              {verifyError}
            </p>
          )}
          {exportError && (
            <p role="alert" className="text-sm text-red-600">
              {exportError}
            </p>
          )}
          {exportNotice && (
            <p role="status" className="text-sm text-muted-foreground">
              {exportNotice}
            </p>
          )}
        </section>
        {(error || bootstrap.error) && (
          <div
            role="alert"
            className="rounded-lg border border-red-300 p-3 text-sm text-red-600"
          >
            <p>{error?.message || bootstrap.error?.message}</p>
            <button
              className="mt-2 underline"
              onClick={() => {
                void bootstrap.mutate();
                void mutate();
              }}
            >
              Try again
            </button>
          </div>
        )}
        {loading && (
          <p className="text-sm text-muted-foreground">Loading audit events…</p>
        )}
        {!loading &&
          !invalidRange &&
          !error &&
          !bootstrap.error &&
          !data?.events?.length && (
            <p className="rounded-lg border p-6 text-sm">
              No events match these filters. Clear filters or send an event with
              the Python SDK or Claude Code collector.
            </p>
          )}
        <div className="space-y-2">
          {data?.events?.map((event) => (
            <details key={event.id} className="rounded-lg border bg-card">
              <summary className="cursor-pointer p-4 text-sm flex flex-wrap gap-3">
                <span className="font-mono">{event.action}</span>
                <span className="text-muted-foreground">{event.actor?.id}</span>
                <span>{event.outcome}</span>
                <time className="ml-auto text-xs text-muted-foreground">
                  {new Date(event.timestamp).toLocaleString()}
                </time>
              </summary>
              <div className="px-4 pb-4">
                {event.resource?.id && (
                  <p className="mb-2 text-xs font-mono break-all">
                    Resource: {event.resource.id}
                  </p>
                )}
                <CapturedPayload label="Event record" value={event} />
              </div>
            </details>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <button
            className={buttonClass}
            disabled={!ready || loading || page <= 1}
            onClick={() => setFilter({ ...filter, page: page - 1 })}
          >
            Previous
          </button>
          <span>
            Page {page} of {totalPages} · {total.toLocaleString()} matching
            events
          </span>
          <button
            className={buttonClass}
            disabled={!ready || loading || page >= totalPages}
            onClick={() => setFilter({ ...filter, page: page + 1 })}
          >
            Next
          </button>
          <label className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
            Events per page
            <select
              className="rounded-lg border bg-background p-2"
              value={pageSize}
              onChange={(e) =>
                updateFilter({ page_size: Number(e.target.value) })
              }
            >
              {[25, 50, 100, 250].map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>
    </>
  );
}
