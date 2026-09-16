import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "License and data handling",
  alternates: { canonical: "/legal" },
};

export default function LegalPage() {
  return (
    <article className="mx-auto max-w-3xl px-6 py-20 space-y-6">
      <h1 className="text-3xl font-semibold">License and data handling</h1>
      <p>
        Fact0 is experimental self-hosted software. The repository LICENSE
        governs its use. It is provided without warranties; consult that license
        for the full terms.
      </p>
      <h2 className="text-xl font-semibold">Your installation, your data</h2>
      <p>
        This edition does not send application telemetry to Fact0 or load
        third-party analytics. The self-hosted application stores owner
        authentication cookies for sign-in. Your PostgreSQL instance stores
        captured events and execution payloads.
      </p>
      <p>
        Python and Claude Code raw capture can include source code, prompts,
        commands, tool results, personal data and secrets. Review the capture
        settings before enabling a collector. The operator is responsible for
        access controls, backups and retention.
      </p>
      <h2 className="text-xl font-semibold">External integrations</h2>
      <p>
        Your agents and Claude Code may call their own model providers or other
        services. Fact0 does not control those services. Documentation and
        GitHub links open external websites.
      </p>
      <a
        className="underline"
        href="https://github.com/fact0-ai/fact0/blob/main/LICENSE"
      >
        Read the source license
      </a>
    </article>
  );
}
