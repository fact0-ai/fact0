# Local dashboard setup

Follow the root README and Docker Compose quickstart. The owner setup command creates one local password account and workspace, then provisions its first write API key. No Fact0 cloud account or OAuth provider is required.

The Go API uses `FACT0_POSTGRES_DSN`; the web process uses `DATABASE_URL` pointing at the same database. Run `fact0-migrate` before owner setup. The migration command accepts fresh databases and known versioned OSS schemas only.

The web proxy exposes `/v1/*` and `/api/v1/*` at the application's origin. Dashboard reads use the local owner's session/JWT; SDKs and the collector use scoped API keys. Manage additional keys in dashboard settings.

Use the repository's Python example and Claude Code plugin instructions to record a run. Open Audit for recorded facts and verification, Executions for spans/replay, and Coding Agents for captured Claude Code sessions. Full values come from supported collector hooks/transcripts; missing or oversized content must remain visibly incomplete.

Evidence exports can be signed with an operator-supplied `FACT0_SIGNING_KEY`. Verify the signature against a separately trusted public key. An intact chain or signed export does not certify compliance, event truth, or complete capture.

Audit timestamps before 1970 or after the end of year 9999 UTC are rejected. Omitted verification/export date bounds include all supported recorded history, including accepted captures whose clocks are ahead of the server. Explicit `from` and `to` values narrow that capture-time window.
