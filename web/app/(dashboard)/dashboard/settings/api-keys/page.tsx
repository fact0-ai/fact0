"use client";

import {
  AlertTriangle,
  Check,
  Copy,
  KeyRound,
  Loader2,
  Plus,
  ShieldOff,
  Trash2,
} from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { docsHref } from "@/lib/docs-origin";
import { useCurrentMember } from "@/lib/use-current-member";
import { useCreateKey, useKeys, useRevokeKey } from "@/lib/use-me";
import { cn } from "@/lib/utils";

function truncateId(id: string): string {
  if (id.length <= 16) return id;
  return `${id.slice(0, 8)}…${id.slice(-4)}`;
}

function formatCreatedAt(iso: string): string {
  try {
    return new Intl.DateTimeFormat(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    }).format(new Date(iso));
  } catch {
    return iso.slice(0, 10);
  }
}

export default function APIKeysPage() {
  const { isAdmin } = useCurrentMember();

  const keys = useKeys();
  const createKey = useCreateKey();
  const revokeKey = useRevokeKey();

  const [newScope, setNewScope] = useState<"read" | "write">("write");
  const [newLabel, setNewLabel] = useState("");
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<{
    id: string;
    key: string;
    scope: string;
    label?: string;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const onCreate = async () => {
    if (!isAdmin) return;
    setCreating(true);
    setError(null);
    try {
      const k = await createKey(newScope, newLabel.trim() || undefined);
      setRevealed({ id: k.id, key: k.key, scope: k.scope, label: k.label });
      setNewLabel("");
      await keys.mutate();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  };

  const onRevoke = async (id: string) => {
    if (!isAdmin) return;
    if (
      !confirm(
        "Revoke this key? Any SDK using it will start receiving 401s immediately.",
      )
    )
      return;
    setRevoking(id);
    setError(null);
    try {
      await revokeKey(id);
      await keys.mutate();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRevoking(null);
    }
  };

  const onCopy = async () => {
    if (!revealed) return;
    try {
      await navigator.clipboard.writeText(revealed.key);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // ignore
    }
  };

  const rows = (keys.data?.keys ?? [])
    .slice()
    .sort((a, b) =>
      a.revoked === b.revoked
        ? a.created_at.localeCompare(b.created_at)
        : a.revoked
          ? 1
          : -1,
    );

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h2 className="text-2xl font-black tracking-tighter text-foreground dark:text-white">
          API Keys
        </h2>
        <p className="text-sm text-zinc-500 max-w-2xl">
          Programmatic access to your audit log. Pass the key in{" "}
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono text-foreground/80">
            Authorization: Bearer …
          </code>
          . Secrets are shown once at creation - we store only the SHA-256 hash.{" "}
          <Link
            href={docsHref("quickstart")}
            className="text-foreground/70 underline underline-offset-2 hover:text-foreground"
          >
            Quickstart guide
          </Link>
        </p>
      </div>

      {!isAdmin && (
        <div className="inline-flex items-center gap-2 rounded-lg border border-amber-500/25 bg-amber-500/5 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
          <ShieldOff className="size-4 shrink-0" />
          Read-only - admin role required to create or revoke keys.
        </div>
      )}

      <div className="overflow-hidden rounded-2xl border border-border/60 bg-card">
        {error && (
          <div className="border-b border-red-500/20 bg-red-500/5 px-5 py-3 text-sm text-red-600 dark:text-red-400">
            {error}
          </div>
        )}

        {revealed && (
          <div className="border-b border-amber-500/25 bg-amber-500/5 px-5 py-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="flex gap-2.5 min-w-0">
                <AlertTriangle className="size-4 shrink-0 text-amber-600 dark:text-amber-400 mt-0.5" />
                <div className="min-w-0 space-y-1">
                  <p className="text-sm font-medium text-foreground">
                    Copy this key now - it won&apos;t be shown again
                  </p>
                  <p className="text-xs text-zinc-500">
                    {revealed.label || "Untitled"} · {revealed.scope} scope
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => setRevealed(null)}
                className="shrink-0 text-sm text-zinc-500 hover:text-foreground transition-colors sm:pt-0.5"
              >
                Done
              </button>
            </div>
            <div className="mt-3 flex items-center gap-2 rounded-lg border border-border/60 bg-muted/30 pl-3 pr-1.5 py-1.5">
              <code className="min-w-0 flex-1 truncate text-xs font-mono text-foreground">
                {revealed.key}
              </code>
              <button
                type="button"
                onClick={onCopy}
                className="inline-flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium text-zinc-600 hover:bg-muted hover:text-foreground transition-colors dark:text-zinc-400"
              >
                {copied ? (
                  <Check className="size-3.5" />
                ) : (
                  <Copy className="size-3.5" />
                )}
                {copied ? "Copied" : "Copy"}
              </button>
            </div>
          </div>
        )}

        {isAdmin && (
          <div
            data-tour="api-keys-section"
            className="border-b border-border/50 px-5 py-5"
          >
            <p className="mb-4 text-sm font-medium text-foreground">
              Create key
            </p>
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-end">
              <div className="space-y-1.5">
                <label htmlFor="key-label" className="text-xs text-zinc-500">
                  Label <span className="text-zinc-400">(optional)</span>
                </label>
                <input
                  id="key-label"
                  value={newLabel}
                  onChange={(e) => setNewLabel(e.target.value)}
                  placeholder="e.g. production-ingestion"
                  className="w-full rounded-lg border border-border/60 bg-muted/30 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/30"
                />
              </div>
              <div className="space-y-1.5">
                <span className="text-xs text-zinc-500">Scope</span>
                <div className="inline-flex rounded-lg border border-border/60 bg-muted/30 p-0.5">
                  {(["read", "write"] as const).map((s) => (
                    <button
                      key={s}
                      type="button"
                      onClick={() => setNewScope(s)}
                      className={cn(
                        "rounded-md px-3 py-1.5 text-sm font-medium capitalize transition-colors",
                        newScope === s
                          ? "bg-indigo-600 text-white"
                          : "text-zinc-500 hover:text-foreground",
                      )}
                    >
                      {s}
                    </button>
                  ))}
                </div>
              </div>
              <button
                type="button"
                onClick={onCreate}
                disabled={creating}
                className="inline-flex items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 transition-colors disabled:opacity-50"
              >
                {creating ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Plus className="size-4" />
                )}
                {creating ? "Creating…" : "Create key"}
              </button>
            </div>
          </div>
        )}

        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-border/50 text-xs text-zinc-500">
                <th className="px-5 py-3 font-medium">Name</th>
                <th className="px-5 py-3 font-medium">Scope</th>
                <th className="px-5 py-3 font-medium">Created</th>
                <th className="px-5 py-3 font-medium">Status</th>
                <th className="px-5 py-3 w-20" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border/40 text-sm">
              {keys.isLoading && (
                <tr>
                  <td
                    colSpan={5}
                    className="px-5 py-12 text-center text-zinc-500"
                  >
                    <Loader2 className="mr-2 inline-block size-4 animate-spin" />
                    Loading keys…
                  </td>
                </tr>
              )}
              {!keys.isLoading && rows.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-5 py-12 text-center">
                    <KeyRound className="mx-auto mb-2 size-8 text-zinc-300 dark:text-zinc-600" />
                    <p className="text-sm text-zinc-500">
                      No API keys yet.
                      {isAdmin
                        ? " Create one above."
                        : " Ask an admin to provision one."}
                    </p>
                  </td>
                </tr>
              )}
              {rows.map((k) => (
                <tr key={k.id} className="hover:bg-muted/20 transition-colors">
                  <td className="px-5 py-3.5">
                    <div className="font-medium text-foreground">
                      {k.label || "Untitled"}
                    </div>
                    <div
                      className="mt-0.5 font-mono text-xs text-zinc-500"
                      title={k.id}
                    >
                      {truncateId(k.id)}
                    </div>
                  </td>
                  <td className="px-5 py-3.5">
                    <span className="inline-flex rounded-md border border-border/60 bg-muted/30 px-2 py-0.5 text-xs font-medium capitalize text-zinc-600 dark:text-zinc-400">
                      {k.scope}
                    </span>
                  </td>
                  <td className="px-5 py-3.5 text-zinc-500">
                    {formatCreatedAt(k.created_at)}
                  </td>
                  <td className="px-5 py-3.5">
                    {k.revoked ? (
                      <span className="text-xs text-zinc-500">
                        Revoked{" "}
                        {k.revoked_at ? formatCreatedAt(k.revoked_at) : ""}
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1.5 text-xs text-zinc-600 dark:text-zinc-400">
                        <span className="size-1.5 rounded-full bg-emerald-500" />
                        Active
                      </span>
                    )}
                  </td>
                  <td className="px-5 py-3.5 text-right">
                    {!k.revoked && isAdmin && (
                      <button
                        type="button"
                        onClick={() => onRevoke(k.id)}
                        disabled={revoking === k.id}
                        title="Revoke key"
                        className="inline-flex items-center justify-center rounded-md p-2 text-zinc-400 hover:bg-red-500/10 hover:text-red-600 transition-colors disabled:opacity-50 dark:hover:text-red-400"
                      >
                        {revoking === k.id ? (
                          <Loader2 className="size-4 animate-spin" />
                        ) : (
                          <Trash2 className="size-4" />
                        )}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
