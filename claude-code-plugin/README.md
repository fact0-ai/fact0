# fact0-claude-code

Stream every Claude Code session into [Fact0](https://fact0.io) as tamper-evident
audit events and execution telemetry — with **zero per-session configuration**.

Once installed, the plugin hooks every Claude Code lifecycle event (prompts, tool
calls, permission decisions, subagents, session start/end), maps it to the Fact0
audit/telemetry schema, and ships it to `api.fact0.io` via a small local collector
binary (`fact0-cc`). Each AI coding session becomes a hash-chained, queryable,
governance-ready fact log.

> Docs: [Claude Code integration](https://docs.fact0.io/integrations/claude-code) —
> install, capture modes, governance, and the full event → schema mapping.

---

## What it does

Claude Code is an AI agent that takes consequential actions — running shell
commands, editing files, opening PRs. Today those actions are invisible to
compliance and audit tooling. This plugin makes them visible:

- **Audit trail for AI-written code** — who (agent vs. human vs. system) did what,
  when, against which resource, with what outcome.
- **Execution telemetry** — each session maps to a Fact0 execution; tool calls
  become spans with causality (including subagent nesting).
- **Governance-ready** — the same `PreToolUse` hook later enables block-and-prove
  policy enforcement.

Architecture is **Option A** from the design doc: each hook is a `command` hook
that invokes the `fact0-cc` collector with the event name as its argument. The
collector reads the Claude Code payload on **stdin**, maps it, and uses the Fact0
SDK (batching, retry, dead-letter, hash chaining) to deliver it. All redaction
happens **client-side, before anything leaves the machine**.

---

## Privacy model

Capture is controlled by `FACT0_CC_CAPTURE_MODE` with three levels:

- **`metadata` (default):** *locator* text ships raw so sessions are readable in
  the dashboard — file paths, command text (first 200 chars), search patterns,
  URLs, and your prompt text. *Content* stays protected: file contents
  (`new_string`/`old_string`), tool outputs, and any other string field ship as
  SHA-256 + length only (numbers like exit codes ship as-is).
- **`hash`:** everything — paths, commands, prompts, contents — ships as
  SHA-256 + length. The audit chain proves *what happened* without exposing any
  text. For locked-down or secret-bearing environments.
- **`raw`:** everything ships as-is, including file contents and tool outputs.
  (Legacy `FACT0_CC_CAPTURE_RAW=1` still selects this mode; an explicit
  `FACT0_CC_CAPTURE_MODE` wins.)

> **Changed default (v0.2):** earlier versions defaulted to hash-only. The
> default is now `metadata`, because hashed file paths and commands made the
> dashboard session view unusable. Set `FACT0_CC_CAPTURE_MODE=hash` to restore
> the previous posture.

---

## Agentless mode (no binary)

If you can't (or don't want to) build and ship the `fact0-cc` collector binary,
the plugin can run in **agentless mode**: instead of `command` hooks that invoke a
local binary, every lifecycle event is delivered straight from Claude Code to the
Fact0 backend over HTTP.

Use `hooks/hooks.http.json` in place of `hooks/hooks.json`. It mirrors the same 8
events and matchers (`SessionStart`, `UserPromptSubmit`, `PreToolUse`,
`PostToolUse`, `Notification`, `SubagentStop`, `Stop`, `SessionEnd`), but each hook
is a Claude Code `"type": "http"` hook that **POSTs the native Claude Code payload**
to:

```
POST ${FACT0_BASE_URL}/v1/integrations/claude-code
Authorization: Bearer ${FACT0_API_KEY}
```

The key (`FACT0_API_KEY`) is passed through via `allowedEnvVars`, and
`FACT0_BASE_URL` defaults to `https://api.fact0.io` (override it for self-hosted /
staging). No local binary, build step, or `bin/fact0-cc` is required — the backend
does the event → Fact0 schema mapping that the collector would otherwise do
locally.

### Tradeoff — read before using

Agentless mode moves all mapping **server-side**, which means the **raw Claude Code
payloads (prompts, tool inputs, tool outputs) leave the machine *before* any local
redaction can run.** There is no client-side hash-only step in front of the HTTP
hook — see the [Privacy model](#privacy-model) above; the hash-only default and
`FACT0_CC_CAPTURE_RAW` opt-in only apply to the binary collector. So agentless mode
is best when:

- you're in a **trusted or CI environment** where sending raw payloads to Fact0 is
  acceptable, or
- you **can't ship a binary** (locked-down hosts, ephemeral runners) and need
  capture anyway.

For untrusted or secret-bearing local environments, prefer the default binary
collector (`hooks/hooks.json`) so redaction happens before anything leaves the
machine.

### Switching to agentless

Point the plugin at the HTTP hooks file (e.g. copy/symlink `hooks/hooks.http.json`
over `hooks/hooks.json`, or reference it from your plugin config), then set:

```bash
export FACT0_API_KEY=...                      # from fact0.io dashboard -> API Keys
export FACT0_BASE_URL=https://api.fact0.io    # optional; self-hosted / staging
```

That's it — no build, no `bin/fact0-cc`. Every session POSTs directly to
`/v1/integrations/claude-code`.

---

## Install

### From GitHub (recommended)

Inside Claude Code — the marketplace manifest lives at the repo root of
[`fact0-ai/fact0`](https://github.com/fact0-ai/fact0):

```
/plugin marketplace add fact0-ai/fact0
/plugin install fact0-claude-code@fact0
```

### From a local checkout

#### 1. Build the collector binary

```bash
bash scripts/build.sh
# -> <repo>/claude-code-plugin/bin/fact0-cc
```

This runs, from `collector/`:

```bash
go mod tidy && go build -o ../bin/fact0-cc .
```

> The build/install scripts are checked in without the executable bit. Make them
> runnable once with `chmod +x scripts/*.sh` (or just invoke them via `bash`).

#### 2. Install the plugin in Claude Code

`scripts/install-local.sh` builds the binary and prints these two commands,
which you run **inside Claude Code** (the marketplace is the repo root, the
plugin lives in `claude-code-plugin/`):

```
/plugin marketplace add /path/to/fact0
/plugin install fact0-claude-code@fact0
```

#### 3. Set your API key

```bash
export FACT0_API_KEY=...   # from fact0.io dashboard -> API Keys
```

After that, every session auto-logs with no further action.

---

## Environment variables

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `FACT0_API_KEY` | ✅ yes | — | Fact0 API key used to authenticate the collector. |
| `FACT0_BASE_URL` | no | `https://api.fact0.io` | Override the Fact0 API endpoint (self-hosted / staging). |
| `FACT0_CC_ACTOR_ID` | no | OS user (`$USER`) | Identity for the human actor on prompt/permission events. |
| `FACT0_CC_ACTOR_EMAIL` | no | — | Email for the human actor (e.g. git email) for audit "who". |
| `FACT0_CC_CAPTURE_MODE` | no | `metadata` | `hash` \| `metadata` \| `raw` — see Privacy model above. |
| `FACT0_CC_CAPTURE_RAW` | no | `0` (off) | Legacy: set to `1` for `raw` mode. `FACT0_CC_CAPTURE_MODE` takes precedence. |
| `FACT0_CC_DISABLED` | no | `0` (off) | Set to `1` to disable the collector entirely (hooks become no-ops). |
| `FACT0_CC_STATE_DIR` | no | `~/.fact0/cc` | Directory for local collector state (session→execution mapping, dead-letter queue). |

---

## What each hook captures

| Claude Code event | Collector arg | Captured |
|---|---|---|
| `SessionStart` | `session-start` | Starts a Fact0 telemetry **execution** keyed by `session_id`; records `cwd`, `source`, `permission_mode`. |
| `UserPromptSubmit` | `prompt` | Audit `claude_code.prompt.submit` — prompt SHA-256 + length (raw text only if `FACT0_CC_CAPTURE_RAW=1`). |
| `PreToolUse` | `pre-tool` | Opens a `TOOL_CALL` span; records `tool_name`, derived resource, and `tool_use_id`. Reserved for future block-and-prove governance. |
| `PostToolUse` | `post-tool` | Closes the span + audit event for the tool result; records input (redacted/hashed) and an output summary from `tool_response`. |
| `Notification` | `notify` | Audit `claude_code.permission.request` (actor = system) for permission prompts / idle notifications via `message`. |
| `SubagentStop` | `subagent-stop` | Nested span / causality edge using `agent_type` and `agent_id`. |
| `Stop` | `stop` | Closes the current turn span. |
| `SessionEnd` | `session-end` | Ends the execution and flushes the batch; records `reason`. |

The collector parses the Claude Code payload into a single `HookInput` struct
whose JSON tags are: `session_id`, `transcript_path`, `cwd`, `permission_mode`,
`hook_event_name`, `tool_name`, `tool_input`, `tool_response`, `tool_use_id`,
`prompt`, `source`, `message`, `agent_type`, `agent_id`, `reason`.

---

## Smoke-testing the binary

`collector/testdata/` contains one realistic hook payload per event. Pipe any of
them into the built binary on stdin:

```bash
bin/fact0-cc prompt      < collector/testdata/prompt.json
bin/fact0-cc pre-tool    < collector/testdata/pre-tool.json
bin/fact0-cc post-tool   < collector/testdata/post-tool.json
bin/fact0-cc session-end < collector/testdata/session-end.json
```

(Set `FACT0_CC_DISABLED=1` to exercise parsing without sending anything to Fact0.)

---

## Build command

```bash
cd collector && go mod tidy && go build -o ../bin/fact0-cc .
```

Or just: `bash scripts/build.sh`.

---

## Telemetry & cost

Every session is modeled as a Fact0 telemetry **execution**, and within it the
collector builds a causality tree out of two kinds of spans:

- **Turn spans** — one per agent turn. A turn opens when the model starts working
  (driven by prompt/tool activity) and closes on the `Stop` hook
  (`fact0-cc stop`). The turn span is the parent for everything that happens while
  the model is acting on a single user request.
- **Tool spans** — one per tool invocation. `PreToolUse` (`fact0-cc pre-tool`)
  opens a `TOOL_CALL` span keyed by `tool_use_id`; `PostToolUse`
  (`fact0-cc post-tool`) closes it. Each tool span is parented to the turn span
  that triggered it, and subagent activity (`SubagentStop`) adds nested
  causality edges so multi-agent work shows up as a proper tree.

This turn-span / tool-span model is what lets Fact0 attribute *which* tool call
happened *during which* turn, and roll cost and duration up the tree.

**Self-healing executions:** if a session has no execution — `SessionStart`
never fired (e.g. the plugin was installed mid-session) or its start call
failed — the next hook that needs one lazily starts it (tagged
`lazy_start: true` in metadata) and persists the id, so telemetry resumes
instead of being silently dropped for the rest of the session. A failed lazy
start is retried by each subsequent hook.

### How per-tool cost is captured

Cost attribution uses two paths, in priority order:

1. **Opportunistic (preferred):** when Claude Code includes cost/usage figures in
   the `PostToolUse` payload, the collector reads them directly off that hook and
   attaches them to the closing tool span — no extra configuration needed.
2. **OpenTelemetry bridge (fallback):** when the `PostToolUse` payload carries no
   cost data, the collector falls back to cost/usage emitted over Claude Code's
   OpenTelemetry metrics. Enable it by exporting:

   ```bash
   export CLAUDE_CODE_ENABLE_TELEMETRY=1
   export OTEL_EXPORTER_OTLP_ENDPOINT=https://otel.fact0.io   # your Fact0 / OTLP collector
   ```

   With telemetry enabled, Claude Code ships token/cost metrics to the configured
   OTLP endpoint (point it at a Fact0 or other OTLP collector), and those figures
   are reconciled back onto the matching tool/turn spans. Without this fallback,
   turns whose cost is not present on `PostToolUse` will simply have no per-tool
   cost recorded.

### Delivery guarantees (dead-letter queue)

Hooks are fire-and-forget, but a network blip no longer loses events. When a
send fails (after the SDK's retries), the collector persists the mapped payload
to `<state_dir>/deadletter/` and redelivers it automatically on a later hook
invocation:

- **What is queued:** audit events (with their *original* timestamp), span
  batches, and end-execution calls. Starting an execution (`SessionStart`) is
  the one call that is not queued — later events need its returned execution id
  immediately, so a failed start is logged and skipped.
- **When replay runs:** at the start of every hook except `pre-tool` (the
  synchronous governance path stays fast), oldest first, capped at 25 letters
  per invocation. Replay stops at the first failure, since the backend is
  likely still unreachable.
- **Poison pills:** a letter that fails 20 deliveries (or is corrupt on disk)
  is renamed `*.abandoned` — kept for inspection, never retried.
- **Visibility:** `fact0-cc status` reports the pending count
  (`dead_letter: N pending`).

Note this makes delivery *at-least-once*: what the queue cannot detect is an
event that was never captured at all (e.g. the collector was killed before the
hook ran) — proving completeness requires transcript reconciliation, which is
future work.

### Checking status

Run the `/fact0-status` slash command inside Claude Code to see whether capture is
active for the current session. It invokes `bin/fact0-cc status` and reports a
one-line summary of whether the collector is enabled, configured
(`FACT0_API_KEY`), and not disabled (`FACT0_CC_DISABLED`).

---

## Governance (block & prove)

Beyond recording what Claude Code did, the plugin can **block disallowed actions
before they happen** — and prove, after the fact, that the block was real and
untampered.

### Synchronous PreToolUse

The `PreToolUse` hook now runs **synchronously** (it is no longer `async`). This
is required for enforcement: Claude Code only honors a deny decision if the hook
returns it on stdout *before* the tool runs. An async hook's output is ignored for
control-flow, so every other lifecycle hook stays async for speed, but `pre-tool`
blocks the tool call just long enough to make a policy decision.

### Enabling enforcement

Enforcement is **off by default**. Turn it on with either (or both) of:

```bash
# Evaluate every tool call against your own policy file:
export FACT0_CC_POLICY_FILE=path/to/policy.json

# Also apply the built-in safety defaults (rm -rf, curl|sh, secret reads, ...):
export FACT0_CC_ENFORCE=1
```

- `FACT0_CC_POLICY_FILE` points at a JSON policy (see schema below). Each tool call
  is matched against the rules; the first matching `deny` rule blocks the call and
  returns its `reason` to Claude Code.
- `FACT0_CC_ENFORCE=1` additionally loads a curated set of built-in safety rules,
  so you get baseline protection even without writing a policy file.

### Fail-open guarantee

Governance is designed to **fail open**: if the policy file is missing, malformed,
unreadable, or the evaluator errors for any reason, the hook does **not** block the
tool — it allows the call and records the failure. Enforcement can never wedge a
developer's session due to a bad config or a collector bug. (A misconfiguration
shows up in the audit log as an allow-with-error span, so it is still visible.)

### Every decision is proven

Each evaluation — allow *or* deny — is recorded as a **`POLICY_EVALUATION`** span
in the Fact0 audit log, carrying the matched rule, action, reason, and the tool
input it was judged against. Because the log is a tamper-evident hash chain, you
can later prove both that a dangerous command was blocked and that no decision was
silently removed or altered.

Verify the chain at any time:

```bash
fact0-cc verify
```

`fact0-cc verify` walks the recorded spans, recomputes each link's hash from the
previous link, and confirms the chain is intact — any insertion, deletion, or edit
of a `POLICY_EVALUATION` (or any other) span breaks the chain and is reported.

### Sample policy

`policy.example.json` (rules are `{match, tool, action, reason}`; `match` is a
regex tested against the tool input, e.g. the Bash command or the edited path):

```json
{
  "rules": [
    {
      "match": "(?i)\\brm\\s+(-[a-z]*r[a-z]*\\s+-[a-z]*f|-rf|-fr)\\b",
      "tool": "Bash",
      "action": "deny",
      "reason": "Recursive force delete (rm -rf) is blocked: destructive and irreversible."
    },
    {
      "match": "(?i)(curl|wget)\\b.*\\|\\s*(ba)?sh\\b",
      "tool": "Bash",
      "action": "deny",
      "reason": "Piping a downloaded script straight into a shell executes unverified remote code."
    },
    {
      "match": "(?i)(^|/)\\.env(\\.[a-z]+)?$",
      "tool": "Write",
      "action": "deny",
      "reason": "Writing .env files is blocked: they hold secrets and credentials."
    }
  ]
}
```

### Sample deny

When Claude Code tries `rm -rf build/` with the policy above, `pre-tool` returns a
deny decision and the call never executes:

```text
PreToolUse: Bash → DENY
  rule:   (?i)\brm\s+(-[a-z]*r[a-z]*\s+-[a-z]*f|-rf|-fr)\b
  reason: Recursive force delete (rm -rf) is blocked: destructive and irreversible.

(recorded as a POLICY_EVALUATION span; verify with `fact0-cc verify`)
```
