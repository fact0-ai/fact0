# Guidance for Fact0 documentation contributors

Describe the experimental self-hosted core and check every API example against the SDK source in this repository. The supported installation has one owner, one workspace, PostgreSQL, a Go API and a Next.js dashboard. Python and Claude Code are the primary walkthroughs.

## Package names and examples

Use the actual public package identities:

- Python: `import fact0`; distribution `fact0-sdk`; local installation `pip install -e ./sdk/python`.
- TypeScript: `import { Fact0Client } from "@fact0/sdk"`.
- Go: `import fact0 "github.com/fact0-ai/fact0/sdk/go"`.

Do not invent wrapper modules or method signatures. Refer to the local quickstarts and SDK tests. Always configure the self-hosted base URL explicitly: `http://localhost:8000` for default local ingestion, or the operator's TLS application origin for remote access through its API proxy. API keys come from owner setup or Settings → API keys; keep their secrets out of documentation and source control.

## Two kinds of records

Audit events record an actor, action, resource, outcome and metadata in a per-workspace hash chain. Valid outcome values are `success`, `failure` and `error`. Telemetry records executions, spans, timestamps, structured details and lifecycle events for inspection and replay. Recording one pipeline does not imply that the other captured every action.

Replay reconstructs stored events. It does not rerun model inference or tools. Chain verification and signed exports detect changes relative to stored hashes and a trusted signing key; they do not establish complete capture, prove source truth, or certify compliance.

## Capture and release boundaries

The Claude collector defaults to raw capture of supported hook/transcript values. Content may include prompts, source code, tool results and secrets. Reduced metadata/hash modes and partial/unavailable status must be explained accurately. Python model capture depends on the integration and configuration.

This release has password login and local owner recovery. Billing, public registration, invitations, hosted administration, Spotlight, email delivery, alert webhooks, public sharing, governance enforcement and OTLP are disabled. A policy-evaluation span can record an agent's own decision; Fact0 does not enforce that decision.

Use plain technical language and concrete examples. Avoid claims of guaranteed capture, regulatory approval, tamper-proof storage, hosted SLAs or production readiness.
