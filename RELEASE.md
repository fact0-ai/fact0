# Fact0 core v0.1.0 — experimental self-hosted release

Self-hosted execution inspection and audit records for AI agents, with Python and Claude Code integrations.

## Release contract

This release supports fresh installations with one local password owner and workspace. Docker Compose runs the Go API, Next.js application and PostgreSQL, with persistent storage and loopback-only exposed ports. No Fact0 cloud account, OAuth, AWS, Neon, Redis or email provider is required.

The supported first-use flow is clone → start → create owner → create an API key → connect Python or Claude Code → inspect activity → verify the recorded audit chain → download an authenticated export. Start with the [README](README.md).

Execution inspection, timelines, DAGs, replay, basic usage information, audit verification and authenticated PDF/evidence exports are included. Billing, founder administration, invitations, Spotlight, email services, external alert webhooks, public sharing, policy enforcement and OTLP are disabled in the UI, routes and associated background work. Records do not expire automatically.

## Reviewed source inclusion

The release branch starts from public repository commit `9272dc24d786e1694aaac7b1ca985e0aeabe90ba`; its public history is preserved. Application code was imported as a reviewed source snapshot, without importing private application Git history.

| Included paths | Review and purpose |
| --- | --- |
| `api/` | Application API, fresh versioned schema, tracked migrations, retained workflow fixes, bundled pricing, license notices and regression tests. |
| `web/` | Existing landing-page design and dashboard, local owner authentication, retained inspector work, bundled fonts, supported navigation and tests. |
| `sdk/` | Existing package identities and release conventions retained. Reviewed Python integration/telemetry working changes included with regression coverage. |
| `claude-code-plugin/`, `.claude-plugin/` | Existing plugin layout retained; collector version 0.3.0, durable capture, explicit binary preparation and release conventions. |
| `openapi/`, `docs/` | Canonical REST schemas and synchronized documentation, local walkthroughs, limitations and operational guidance. |
| `compose.yaml`, `scripts/`, `examples/`, `.github/workflows/` | Local setup, backup/export verification, deterministic example and public validation/release automation. |

Excluded: private Git history, environment files and generated credentials, founder documents, local research/readiness reports, backups and databases, caches/build outputs, production ECS/Vercel deployment configuration and promotional customer/testimonial assets. Original application and public-repository working changes were preserved in their original checkouts. The release source and public history are scanned for secrets before publishing.

The existing MIT license applies to Fact0 source. Font and dependency licenses retain their original terms; see [third-party notices](THIRD_PARTY_NOTICES.md). Collector binary releases include their license notices.

## Behavior and compatibility

- `/api/v1` telemetry and `/v1` audit interfaces remain. Optional execution `started_at` and `ended_at` preserve occurrence times when records arrive late; omitted values use server time.
- Audit hashes use the documented `sha256:v2:` canonical JSON format, including tenant, sequence, actor/resource types, metadata, database-precision timestamps and the preceding hash. There is no supported chain-rewriting endpoint.
- Ingestion commits synchronously. References must belong to the same tenant and execution. Duplicate deliveries preserve identities; running parents can complete without rewriting their lifecycle history.
- Session filtering and pagination operate on the server. The inspector exposes full supported content with copy/download controls; shortened text is for display only.
- Claude raw capture is the default, with API redaction disabled. Prompts, source and tool output are stored locally. Hash and metadata modes remain available.
- Collector hooks journal locally and do not enforce policy or block Claude actions on capture failure. Prepare the binary before starting Claude. Request batches fit within 4 MiB; oversized records remain visibly failed. Pending records are never silently evicted from the default 256 MiB spool.
- Go SDK dynamic JSON numbers are now `json.Number`, preserving large integers and exact decimal values when re-encoded. Callers doing direct float64 type assertions should update those assertions.

## Validation

Public CI runs API tests against PostgreSQL and race checks, frontend lint/type checks and production build, affected Python/Go/TypeScript SDK and collector tests, schema/documentation checks, and a fresh Compose smoke test with backup/restore.

The regression suite covers cross-tenant references, duplicate deliveries, parent/child lifecycle updates, late events, audit-field tampering, long sessions, concurrent hooks, HTTP-200 item failures, outages/recovery, Unicode, large JSON integers, long output and request-size boundaries. The local release check also exercises a real Claude Code session with successful and deliberately failing tools, then checks capture, replay and audit exports.

The candidate passed [public core CI](https://github.com/fact0-ai/fact0/actions/runs/35073725779), including all four jobs and the fresh Docker backup/restore gate. Local browser checks verified owner login, Python graph/replay, audit verification/export and expandable Claude content.

A fresh Claude Code 2.1.273 session passed exact comparison with its source transcript: five assistant messages (819 characters), 9,528 input / 479 output tokens, full 13,102-character tool stdout, Unicode, the integer `9007199254740993`, and the intentional tool failure. Its execution/replay duration matched the captured 10,982 ms. The queue emptied, and the six-event session export passed date-filtered chain and trusted-signature verification. An offline replay fixture also preserved the original 23,698 ms capture interval and all content.

These are synthetic acceptance runs, not customer adoption or production-performance claims.

## Limits and maintenance

This is an experimental release with best-effort maintenance and no SLA. It establishes the supported workflows above; it is not a production-readiness claim for every deployment.

Capture depends on supported Claude hook/transcript formats, local filesystem access and available spool/disk space. Missing, malformed, unsupported or incomplete transcripts must be reported as partial or unavailable. Hooks cannot prove that all real-world activity was recorded. Windows collector binaries are not provided.

A valid chain establishes integrity of recorded entries, not completeness, truth, agent safety or regulatory compliance. A signed export binds the PDF and verification report to a separately trusted instance key. A storage/key administrator can replace an entire history and its keys. PDF/evidence reports are not a database backup; use the documented PostgreSQL backup/restore procedure to preserve a full instance.

Raw capture can contain sensitive content. Protect the installation, `.env`, collector state and backups. Monitor storage growth. Password reset revokes browser sessions; existing API JWTs expire within 15 minutes and API keys require separate revocation. There is no migration from the former hosted installation.
