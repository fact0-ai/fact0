import {
  ArrowRight,
  Code2,
  TerminalSquare,
  GitBranch,
  ShieldCheck,
  Search,
} from "lucide-react";
import { docsHref } from "@/lib/docs-origin";
export default function Home() {
  return (
    <>
      <section className="mx-auto max-w-6xl px-6 pt-20 pb-24">
        <div className="inline-flex rounded-full border px-3 py-1 text-xs text-muted-foreground">
          Open source · Experimental · Runs on your infrastructure
        </div>
        <div className="grid lg:grid-cols-2 gap-14 mt-8 items-center">
          <div>
            <h1 className="text-5xl md:text-6xl font-semibold tracking-tight leading-[1.08]">
              See what your
              <br />
              agents actually did.
            </h1>
            <p className="mt-6 max-w-lg text-lg text-muted-foreground leading-relaxed">
              Capture Python agent runs and Claude Code sessions. Inspect
              prompts, tool calls and results, replay recorded traces, and
              verify the audit chain in one local dashboard.
            </p>
            <div className="mt-8 flex flex-wrap gap-3">
              <a
                href="https://github.com/fact0-ai/fact0#quickstart"
                className="inline-flex items-center gap-2 rounded-lg bg-foreground px-5 py-3 text-sm font-medium text-background"
              >
                Run locally <ArrowRight className="size-4" />
              </a>
              <a
                href={docsHref()}
                className="rounded-lg border px-5 py-3 text-sm font-medium"
              >
                Read the docs
              </a>
            </div>
            <p className="mt-5 text-xs text-muted-foreground">
              One owner. PostgreSQL. No cloud account, subscription, or API key
              from Fact0.
            </p>
          </div>
          <div className="rounded-2xl border bg-card shadow-sm overflow-hidden">
            <div className="border-b px-5 py-4 flex justify-between text-xs font-mono text-muted-foreground">
              <span>SESSION / example</span>
              <span>RECORDED TRACE</span>
            </div>
            <div className="p-6 space-y-5">
              {[
                ["01", "Prompt", "Investigate the failing test"],
                ["02", "Read", "src/agent.py"],
                ["03", "Bash", "pytest tests/test_agent.py"],
                ["04", "Response", "The tool result is missing a field"],
              ].map(([n, title, body]) => (
                <div key={n} className="flex gap-4">
                  <span className="size-7 flex items-center justify-center rounded-full border text-[10px] text-muted-foreground shrink-0">
                    {n}
                  </span>
                  <div>
                    <div className="text-xs font-semibold">{title}</div>
                    <div className="mt-1 text-sm text-muted-foreground font-mono">
                      {body}
                    </div>
                  </div>
                </div>
              ))}
            </div>
            <div className="border-t px-5 py-3 text-[11px] text-muted-foreground">
              Illustrative example · your dashboard shows captured data
            </div>
          </div>
        </div>
      </section>
      <section className="border-y bg-muted/20">
        <div className="mx-auto max-w-6xl px-6 py-16">
          <h2 className="text-3xl font-semibold tracking-tight">
            Two ways in. One history.
          </h2>
          <div className="grid md:grid-cols-2 gap-6 mt-8">
            {[
              [
                Code2,
                "Python agents",
                "Instrument your own agents with the Python SDK. Capture execution spans, model inputs and outputs, and audit events.",
                "sdk/python/installation",
              ],
              [
                TerminalSquare,
                "Claude Code",
                "Connect the collector to coding sessions. Inspect submitted prompts, tool inputs and outputs, assistant text and capture gaps.",
                "integrations/claude-code",
              ],
            ].map(([Icon, title, body, path]) => {
              const I = Icon as typeof Code2;
              return (
                <a
                  key={String(title)}
                  href={docsHref(String(path))}
                  className="rounded-xl border bg-card p-7 hover:border-zinc-400"
                >
                  <I className="size-5" />
                  <h3 className="mt-5 text-xl font-semibold">
                    {String(title)}
                  </h3>
                  <p className="mt-3 text-sm text-muted-foreground leading-relaxed">
                    {String(body)}
                  </p>
                  <span className="mt-5 inline-block text-sm underline">
                    Integration guide
                  </span>
                </a>
              );
            })}
          </div>
        </div>
      </section>
      <section className="mx-auto max-w-6xl px-6 py-20">
        <div className="grid md:grid-cols-3 gap-10">
          {[
            [
              Search,
              "Inspect the evidence",
              "Search recorded actions and expand captured inputs, outputs and metadata. Copy or download payloads without a preview cutoff.",
            ],
            [
              GitBranch,
              "Replay the trace",
              "Walk through stored execution events with a graph and timing waterfall. Replay does not rerun tools or model inference.",
            ],
            [
              ShieldCheck,
              "Check the history",
              "Recompute the SHA-256 audit chain to detect changed stored records. Capture completeness remains a separate concern.",
            ],
          ].map(([Icon, title, body]) => {
            const I = Icon as typeof Search;
            return (
              <div key={String(title)}>
                <I className="size-5" />
                <h3 className="mt-4 text-lg font-semibold">{String(title)}</h3>
                <p className="mt-3 text-sm text-muted-foreground leading-relaxed">
                  {String(body)}
                </p>
              </div>
            );
          })}
        </div>
        <div className="mt-16 rounded-xl border p-6 text-sm text-muted-foreground leading-relaxed">
          <strong className="text-foreground">An experimental release.</strong>{" "}
          APIs and storage may change. Raw capture can contain secrets, personal
          data and source code. You control deployment, access, retention and
          backups. This release is not a compliance certification or an
          enforcement system.
        </div>
      </section>
    </>
  );
}
