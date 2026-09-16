// Demo fixtures for the Coding Agents view: four narrative Claude Code
// sessions plus their audit-event timelines, so prospects experience the
// session list, timeline, denials, and responses without installing anything.

import type { AuditEvent } from "../audit-types";
import type {
  ClaudeCodeSession,
  ClaudeCodeSessionFilter,
  ClaudeCodeSessionListResponse,
} from "../claude-code-api";

const DEMO_ACTOR = "priya";

function minsAgo(m: number): string {
  return new Date(Date.now() - m * 60_000).toISOString();
}

interface DemoSessionSpec {
  id: string;
  cwd: string;
  branch: string;
  status: "active" | "idle" | "completed";
  startedMinsAgo: number;
  endedMinsAgo?: number;
  turns: Array<{
    prompt: string;
    tools: Array<{
      tool: string;
      type: "file" | "shell.command" | "claude_code.tool";
      locator: string;
      failed?: boolean;
    }>;
    denial?: { tool: string; reason: string };
    response: string;
    tokensOut: number;
  }>;
}

const SPECS: DemoSessionSpec[] = [
  {
    id: "demo-cc-session-1",
    cwd: "/Users/priya/work/payments-service",
    branch: "fix/refund-race",
    status: "active",
    startedMinsAgo: 22,
    turns: [
      {
        prompt: "the refund worker double-processes when two webhooks land at once — find and fix the race",
        tools: [
          { tool: "grep", type: "claude_code.tool", locator: "Grep" },
          { tool: "read", type: "claude_code.tool", locator: "Read" },
          { tool: "edit", type: "file", locator: "/Users/priya/work/payments-service/internal/refund/worker.go" },
          { tool: "bash", type: "shell.command", locator: "go test ./internal/refund/... -race -count=1" },
        ],
        response:
          "Found it: two webhook deliveries for the same refund id both passed the status check before either wrote. I added a SELECT ... FOR UPDATE row lock in ProcessRefund and a unique constraint on (refund_id, attempt). The race test now passes with -race.",
        tokensOut: 1840,
      },
      {
        prompt: "great — clean up the retry loop too and run the full suite",
        tools: [
          { tool: "edit", type: "file", locator: "/Users/priya/work/payments-service/internal/refund/retry.go" },
          { tool: "bash", type: "shell.command", locator: "go test ./... -count=1", failed: true },
          { tool: "edit", type: "file", locator: "/Users/priya/work/payments-service/internal/refund/retry_test.go" },
          { tool: "bash", type: "shell.command", locator: "go test ./... -count=1" },
        ],
        denial: {
          tool: "Bash",
          reason: "Recursive force delete (rm -rf) is blocked: destructive and irreversible.",
        },
        response:
          "Retry loop now uses exponential backoff with jitter instead of the fixed 1s sleep. One existing test asserted the old timing — updated it. Full suite green. (Note: my attempt to clear the build cache with rm -rf was blocked by policy, so I used go clean instead.)",
        tokensOut: 1420,
      },
    ],
  },
  {
    id: "demo-cc-session-2",
    cwd: "/Users/priya/work/checkout-web",
    branch: "feat/apple-pay",
    status: "completed",
    startedMinsAgo: 260,
    endedMinsAgo: 180,
    turns: [
      {
        prompt: "add Apple Pay as a payment method behind the existing PaymentProvider interface",
        tools: [
          { tool: "read", type: "claude_code.tool", locator: "Read" },
          { tool: "write", type: "file", locator: "/Users/priya/work/checkout-web/src/payments/apple-pay.ts" },
          { tool: "edit", type: "file", locator: "/Users/priya/work/checkout-web/src/payments/registry.ts" },
          { tool: "edit", type: "file", locator: "/Users/priya/work/checkout-web/src/checkout/PaymentSelector.tsx" },
          { tool: "bash", type: "shell.command", locator: "npx tsc --noEmit && npm test -- payments" },
        ],
        response:
          "Apple Pay is wired in behind PaymentProvider: new apple-pay.ts implements canMakePayments detection and the merchant-validation handshake, registry.ts registers it, and PaymentSelector shows it only when the device supports it. Typecheck and payment tests pass.",
        tokensOut: 2310,
      },
    ],
  },
  {
    id: "demo-cc-session-3",
    cwd: "/Users/priya/work/payments-service",
    branch: "main",
    status: "completed",
    startedMinsAgo: 1500,
    endedMinsAgo: 1470,
    turns: [
      {
        prompt: "walk me through how the ledger reconciliation job works — don't change anything",
        tools: [],
        response:
          "The reconciliation job runs hourly: it pulls settled transactions from the processor API, matches them against ledger entries by (charge_id, amount), and writes discrepancies to recon_exceptions. Unmatched entries older than 48h page the on-call. The interesting subtlety is the two-phase matching — exact first, then amount-tolerant within ±1 cent for FX rounding.",
        tokensOut: 980,
      },
    ],
  },
  {
    id: "demo-cc-session-4",
    cwd: "/Users/priya/work/infra",
    branch: "chore/tf-upgrade",
    status: "idle",
    startedMinsAgo: 95,
    turns: [
      {
        prompt: "upgrade the terraform aws provider to v5 and fix whatever breaks in plan",
        tools: [
          { tool: "edit", type: "file", locator: "/Users/priya/work/infra/versions.tf" },
          { tool: "bash", type: "shell.command", locator: "terraform init -upgrade" },
          { tool: "bash", type: "shell.command", locator: "terraform plan -out=tf.plan", failed: true },
          { tool: "edit", type: "file", locator: "/Users/priya/work/infra/modules/s3/main.tf" },
          { tool: "bash", type: "shell.command", locator: "terraform plan -out=tf.plan" },
        ],
        response:
          "Provider bumped to ~> 5.0. The v5 breaking change that bit us: aws_s3_bucket inline ACLs are gone — moved them to aws_s3_bucket_acl resources in the s3 module. Plan is clean now: 4 to change, 0 to destroy.",
        tokensOut: 1660,
      },
    ],
  },
];

function specToSession(s: DemoSessionSpec): ClaudeCodeSession {
  const tools = s.turns.flatMap((t) => t.tools);
  const files = new Set(tools.filter((t) => t.type === "file").map((t) => t.locator));
  return {
    session_id: s.id,
    cwd: s.cwd,
    source: "startup",
    permission_mode: "default",
    actor_id: DEMO_ACTOR,
    started_at: minsAgo(s.startedMinsAgo),
    ended_at: s.endedMinsAgo !== undefined ? minsAgo(s.endedMinsAgo) : undefined,
    duration_s: (s.startedMinsAgo - (s.endedMinsAgo ?? Math.max(s.startedMinsAgo - 45, 2))) * 60,
    status: s.status,
    prompts: s.turns.length,
    tool_calls: tools.length,
    files_touched: files.size,
    commands: tools.filter((t) => t.type === "shell.command").length,
    failures: tools.filter((t) => t.failed).length,
    git_branch: s.branch,
    denied: s.turns.filter((t) => t.denial).length,
    cost_usd: Math.round(s.turns.reduce((acc, t) => acc + t.tokensOut * 0.000015, 0) * 1000) / 1000 + 0.4,
    tokens: s.turns.reduce((acc, t) => acc + t.tokensOut, 0) + 250_000,
    execution_id: undefined,
    last_activity_at: minsAgo(s.endedMinsAgo ?? Math.max(s.startedMinsAgo - 45, 2)),
  };
}

export const DEMO_CC_SESSIONS: ClaudeCodeSession[] = SPECS.map(specToSession);

export function listDemoClaudeCodeSessions(
  filter: ClaudeCodeSessionFilter = {},
): ClaudeCodeSessionListResponse {
  let sessions = DEMO_CC_SESSIONS;
  if (filter.cwd) sessions = sessions.filter((s) => s.cwd === filter.cwd);
  return { sessions, total: sessions.length, page: 1, page_size: filter.page_size ?? 50 };
}

export function getDemoClaudeCodeSession(id: string): ClaudeCodeSession | undefined {
  return DEMO_CC_SESSIONS.find((s) => s.session_id === id);
}

/** Timeline audit events for the demo sessions, newest first. */
export function demoClaudeCodeEvents(filter: {
  action?: string;
  resource_id?: string;
  page_size?: number;
}): { events: AuditEvent[]; total: number; page: number; page_size: number } {
  const events: AuditEvent[] = [];
  let seq = 90_000;

  const push = (
    spec: DemoSessionSpec,
    at: string,
    action: string,
    resource: AuditEvent["resource"],
    actorType: "human" | "agent" | "system",
    outcome: string,
    metadata: Record<string, unknown>,
  ) => {
    seq++;
    events.push({
      id: `evt_demo_cc_${seq}`,
      tenant_id: "demo",
      timestamp: at,
      actor: { id: actorType === "agent" ? "claude-code" : DEMO_ACTOR, type: actorType },
      action,
      resource,
      outcome,
      metadata: { session_id: spec.id, ...metadata },
      prev_hash: "demo",
      hash: "demo",
      sequence_number: seq,
    } as unknown as AuditEvent);
  };

  for (const spec of SPECS) {
    const sessionRes = { id: spec.id, type: "claude_code.session", name: "" };
    let t = spec.startedMinsAgo;
    push(spec, minsAgo(t), "claude_code.session.start", sessionRes, "human", "success", {
      cwd: spec.cwd,
      git_branch: spec.branch,
    });
    for (const turn of spec.turns) {
      t -= 1;
      push(spec, minsAgo(t), "claude_code.prompt.submit", sessionRes, "human", "success", {
        prompt: turn.prompt,
        prompt_len: turn.prompt.length,
      });
      for (const tool of turn.tools) {
        t -= 0.7;
        push(
          spec,
          minsAgo(t),
          `claude_code.tool.${tool.tool}`,
          {
            id: tool.locator,
            type: tool.type,
            name: tool.type === "file" ? tool.locator.split("/").pop() ?? "" : tool.locator,
          },
          "agent",
          tool.failed ? "error" : "success",
          {},
        );
      }
      if (turn.denial) {
        t -= 0.3;
        push(spec, minsAgo(t), "claude_code.permission.denied", sessionRes, "human", "failure", {
          tool_name: turn.denial.tool,
          decision: "denied",
          reason: turn.denial.reason,
        });
      }
      t -= 0.4;
      push(spec, minsAgo(t), "claude_code.turn.complete", sessionRes, "agent", "success", {
        response: turn.response,
        response_len: turn.response.length,
        output_tokens: turn.tokensOut,
        model: "claude-sonnet-4-5",
      });
    }
    if (spec.endedMinsAgo !== undefined) {
      push(spec, minsAgo(spec.endedMinsAgo), "claude_code.session.end", sessionRes, "human", "success", {
        reason: "exit",
      });
    }
  }

  let out = events;
  if (filter.resource_id) {
    out = out.filter(
      (e) => e.resource.id === filter.resource_id || e.metadata?.["session_id"] === filter.resource_id,
    );
  }
  if (filter.action) {
    const prefix = filter.action.replace(/\.?\*$/, "");
    out = out.filter((e) => e.action.startsWith(prefix));
  }
  out = [...out].sort((a, b) => b.timestamp.localeCompare(a.timestamp));
  const pageSize = filter.page_size ?? 50;
  return { events: out.slice(0, pageSize), total: out.length, page: 1, page_size: pageSize };
}
