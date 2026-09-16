# Fact0 for Claude Code

Record Claude Code sessions in your self-hosted Fact0 instance. The collector captures prompts, tool inputs and outputs, errors, permission requests, notifications, and the supported assistant content available in Claude's local transcript. A session links audit events to execution spans.

**Raw capture is the default.** Prompts, file contents, commands, tool outputs, and transcript content can contain secrets. This release defaults to `http://localhost:8000`; choose the destination before starting Claude Code. Remote policy fetching and tool enforcement are not included.

## Local setup

Start Fact0 using the repository's [quickstart](../README.md#quickstart), then create an API key in the dashboard. Building from source requires the Go version declared in `collector/go.mod`. From the repository root:

```sh
bash claude-code-plugin/scripts/build.sh
export FACT0_BASE_URL=http://localhost:8000
export FACT0_API_KEY=your-local-api-key
claude-code-plugin/bin/fact0-cc prepare
claude --plugin-dir ./claude-code-plugin
```

Alternatively, install from the marketplace inside Claude Code:

```text
/plugin marketplace add /absolute/path/to/fact0
/plugin install fact0-claude-code@fact0
```

The repository root contains `.claude-plugin/marketplace.json`; the installable plugin lives in `claude-code-plugin`. Restart Claude Code after installation or environment changes.

For a published release, run the installed plugin's `bin/fact0-cc prepare` once before starting your first captured session. It downloads the platform binary and verifies the release checksum. Source checkouts should use `scripts/build.sh`, particularly before a release is published. Hooks never download binaries. macOS and Linux on arm64 and amd64 are supported.

## Inspect and retry

```sh
claude-code-plugin/bin/fact0-cc status
claude-code-plugin/bin/fact0-cc flush
claude-code-plugin/bin/fact0-cc verify
```

`status` reads local configuration, pending hooks, permanent delivery failures, and the last capture error. `flush` retries the queue. `verify` displays the backend's `valid` and `events_checked` result. A valid chain verifies recorded events; it does not prove that every Claude action was captured.

## Capture and delivery

Each hook synchronously writes an owner-readable journal, then launches a detached delivery worker. Hooks emit no decision payload and return success even if capture fails. Network requests run only in the worker. The short local append preserves hook ordering and does not wait for the backend.

The worker serializes state across processes. Execution creation uses a persisted idempotency key. Audit events and span operations retain their IDs and exact payloads through retries. The collector checks per-item acceptance even when HTTP status is 200. Turn and tool spans are recorded as `RUNNING` before their children or completion updates; incomplete spans are marked `CANCELLED` when the session ends without a completion hook.

On Stop, the collector snapshots the current turn from the local JSONL transcript. SubagentStop also snapshots the child transcript when Claude supplies `agent_transcript_path`. It preserves ordered assistant message content and joins every supported text block in order. Tool result records do not create a new user turn. A missing or unreadable transcript emits `capture_status: unavailable`; malformed records or a clipped read window emit `partial`. The read window is 256 MiB. Only content present in the supported hook or transcript format can be captured.

The default spool budget is **256 MiB**, with files `0600` inside directories `0700`. The queue contains raw local hook data even when a reduced network capture mode is selected. Existing records are never evicted to make room. A full or unwritable spool rejects the new hook and writes a visible capture error when possible; capture cannot be complete in that condition. Protect and back up this directory according to your own data policy.

Requests are split at item boundaries and capped at **4 MiB**. A single oversized item is preserved intact as a permanent delivery failure, not truncated or silently dropped. A permanent failure holds later hooks for that session. Configuration changes permit retry; raising a lower client request cap may resolve it, but an item exceeding the server's 4 MiB limit requires manual repair or export from the retained journal. Capture mode is saved with each hook; changing it affects new hooks and does not rewrite queued payloads. Fix the cause before retrying. Use a separate state directory for different backend instances or tenants. Transient failures retry on subsequent hooks or `flush`; keeping Claude open does not itself schedule periodic retries.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `FACT0_API_KEY` | required | API key for your instance |
| `FACT0_BASE_URL` | `http://localhost:8000` | Destination API origin |
| `FACT0_CC_CAPTURE_MODE` | `raw` | `raw`, `metadata`, or `hash` |
| `FACT0_CC_ACTOR_ID` | OS user | Human actor identity |
| `FACT0_CC_ACTOR_EMAIL` | empty | Optional actor email |
| `FACT0_CC_STATE_DIR` | `~/.fact0/cc` | Private queue and session state |
| `FACT0_CC_SPOOL_MAX_BYTES` | `268435456` | Aggregate pending-hook budget |
| `FACT0_CC_REQUEST_MAX_BYTES` | `4194304` | Client request cap; cannot exceed 4 MiB |
| `FACT0_CC_DISABLED` | off | Disable collection |
| `FACT0_CC_BIN` | unset | Explicit prebuilt collector path |

`metadata` retains prompt text and bounded locator/assistant previews while hashing tool content and output strings. `hash` replaces prompt, tool, and response text with hashes and lengths. JSON numbers are preserved without floating-point conversion, including during journal replay. These modes reduce outbound content; they are not secret detection or redaction policies.

This release intentionally excludes the old remote governance and HTTP-only hook paths. Legacy dead-letter files from earlier collector versions are reported separately by `status`; they are not automatically migrated into the new journal.

## Development

```sh
cd claude-code-plugin/collector
go test -race ./...
go vet ./...
```

Tests cover complete raw capture, Unicode and large JSON numbers, missing and malformed transcripts, ordering of parent spans, concurrent hooks, crash replay, batch rejection, bounded queues, and explicit binary preparation.
