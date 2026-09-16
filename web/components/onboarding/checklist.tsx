"use client";
import Link from "next/link";
import { docsHref } from "@/lib/docs-origin";
import { useBootstrap } from "@/lib/use-me";
import { CapturedPayload } from "@/components/inspector/captured-payload";
export function OnboardingChecklist() {
  const bootstrap = useBootstrap();
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Connect your first agent</h1>
      <p className="text-sm text-muted-foreground">
        Your owner workspace is ready. Create an API key, then send a Python
        event or connect Claude Code. The dashboard shows activity as records
        arrive.
      </p>
      {bootstrap.error && <p role="alert">{bootstrap.error.message}</p>}
      {bootstrap.data?.key && (
        <CapturedPayload
          label="Initial API key — save now"
          value={bootstrap.data.key}
        />
      )}
      <Link
        className="inline-block rounded-lg bg-foreground text-background px-4 py-2 text-sm"
        href="/dashboard/settings/api-keys"
      >
        Manage API keys
      </Link>
      <div className="grid md:grid-cols-2 gap-4">
        <a className="rounded-xl border p-5" href={docsHref("sdk/python/installation")}>
          <h2 className="font-semibold">Python SDK</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Instrument an agent and point its base URL at this installation.
          </p>
        </a>
        <a
          className="rounded-xl border p-5"
          href={docsHref("integrations/claude-code")}
        >
          <h2 className="font-semibold">Claude Code collector</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Capture prompts, tool inputs and results. Review raw-capture data
            handling before starting.
          </p>
        </a>
      </div>
      <p className="text-xs text-muted-foreground">
        Use the public URL of your own installation as FACT0_BASE_URL. Never
        send a local key to another host.
      </p>
    </div>
  );
}
