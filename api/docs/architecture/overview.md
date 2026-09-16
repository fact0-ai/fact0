# Fact0 core architecture

The self-hosted release runs a Go REST API, a Next.js dashboard with one local password owner, and PostgreSQL. The Python SDK and Claude Code collector send records to the same API. There is no object-storage dependency.

Two related data paths are retained:

- `/v1/events` and `/v1/events/batch` synchronously append audit records to a per-workspace `sha256:v2:` chain. Verification checks the integrity of ingested records, not their truth or completeness.
- `/api/v1/executions` stores executions, spans, and append-only lifecycle/event history. A RUNNING span can complete once. Span projections support inspection; immutable lifecycle snapshots retain the original records. Parent, causality, and event references must stay within one execution.

Replay orders recorded frames by timestamp, with ingestion sequence as a tie breaker. It does not call models or tools again. The DAG reconstructs supplied parent and causal relationships.

Payloads are inline in PostgreSQL. The request limit is 4 MiB of encoded JSON. Raw capture is the default; optional configured redaction happens before audit hashing. Batch responses can contain per-item rejections even with HTTP 200.

Audit capture timestamps must fall between `1970-01-01T00:00:00Z` and `9999-12-31T23:59:59.999999999Z`, inclusive; an omitted timestamp uses server time. Explicit values outside that range are rejected in single and batch ingestion. Verification and PDF/evidence exports with omitted date bounds cover the full supported recorded history, including future-dated captures already received. Supply `from` or `to` to restrict the capture-time window. Timestamps are stored at microsecond precision.

`FACT0_OSS_MODE=true` is the default supported profile. It disables billing, admin, invitations, sharing, governance policy, Spotlight, alerts, OTLP, retention/deletion workers, asynchronous ingestion, and outbound pricing synchronization. Static cost estimates remain available. Core features have no paid-plan quotas.

`fact0-migrate` uses `FACT0_POSTGRES_DSN`. It installs the reviewed fresh baseline, including BetterAuth tables, records checksums, and applies ordered forward migrations. Unknown existing schemas and altered migration history are rejected. Do not replay the historical SQL migration directory manually.

See the root README for installation and `openapi/` for the supported REST contracts.
