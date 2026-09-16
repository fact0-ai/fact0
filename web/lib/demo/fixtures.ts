import type { AuditEvent, VerifyResult } from "@/lib/audit-types";
import type {
  AlertSettings,
  BootstrapResponse,
  InstrumentationResponse,
  KeyRow,
  MePlanResponse,
  MeStatus,
  MeUsage,
  ShareLink,
} from "@/lib/me-api";
import type {
  DAGEdge,
  DAGNode,
  Execution,
  ReplayFrame,
  ReplayResponse,
  Span,
} from "@/lib/types";

export const DEMO_TENANT_ID = "tnt_demo_acme_finance";
export const DEMO_WORKSPACE_NAME = "Acme Finance -Support Copilot";
export const DEMO_SHARE_ID = "demo";
export const DEMO_PRIMARY_EXECUTION_ID = "demo-exec-failed-pay-001";

/** Production-scale demo volumes (Acme Finance -30d window). */
export const DEMO_CHAIN_HEIGHT = 31_847;
export const DEMO_TOTAL_EXECUTIONS = 2_847;
export const DEMO_TOTAL_SPANS = 14_256;

const now = Date.now();
const hoursAgo = (h: number) => new Date(now - h * 3600_000).toISOString();
const daysAgo = (d: number) => new Date(now - d * 86400_000).toISOString();

export const DEMO_MEMBERS = [
  {
    id: "mem-demo-owner",
    role: "owner",
    createdAt: daysAgo(45),
    user: {
      id: "user-demo-jordan",
      name: "Jordan Lee",
      email: "jordan@acmefinance.com",
    },
  },
  {
    id: "mem-demo-admin",
    role: "admin",
    createdAt: daysAgo(30),
    user: {
      id: "user-demo-sam",
      name: "Sam Rivera",
      email: "sam.rivera@acmefinance.com",
    },
  },
] as const;

export const DEMO_STATUS: MeStatus = {
  tenant_id: DEMO_TENANT_ID,
  has_keys: true,
  has_activity: true,
  event_count: DEMO_CHAIN_HEIGHT,
  chain_height: DEMO_CHAIN_HEIGHT,
  onboarding_step: 0,
};

export const DEMO_PLAN: MePlanResponse = {
  plan: {
    id: "pro",
    name: "Pro",
    retention_days: 90,
    description: "Pro tier with 90-day retention.",
  },
  retention_days: 90,
  monthly_event_limit: 200000,
};

export const DEMO_USAGE: MeUsage = {
  window_days: 30,
  audit_events: DEMO_CHAIN_HEIGHT,
  audit_events_this_month: 18420,
  monthly_event_limit: 200000,
  usage_pct: 9.2,
  executions_total: DEMO_TOTAL_EXECUTIONS,
  spans_total: DEMO_TOTAL_SPANS,
  tenant_scoped: true,
};

export const DEMO_BOOTSTRAP: BootstrapResponse = {
  tenant: {
    id: DEMO_TENANT_ID,
    name: DEMO_WORKSPACE_NAME,
    created_at: daysAgo(45),
  },
  key_id: "key_demo_write_001",
  scope: "write",
  provisioned: false,
};

export const DEMO_KEYS: KeyRow[] = [
  {
    id: "key_demo_write_001",
    scope: "write",
    label: "Production agent",
    created_at: daysAgo(40),
    revoked: false,
  },
  {
    id: "key_demo_read_002",
    scope: "read",
    label: "Compliance read-only",
    created_at: daysAgo(12),
    revoked: false,
  },
];

export const DEMO_SHARE_LINKS: ShareLink[] = [
  {
    id: DEMO_SHARE_ID,
    tenant_id: DEMO_TENANT_ID,
    label: "Q2 security review",
    expires_at: new Date(now + 90 * 86400_000).toISOString(),
    access_count: 14,
    filter_from: daysAgo(90),
    filter_to: new Date().toISOString(),
    created_at: daysAgo(7),
  },
];

export const DEMO_ALERT_SETTINGS: AlertSettings = {
  alert_webhook_url: "https://hooks.acme-finance.internal/fact0/alerts",
};

export const DEMO_INSTRUMENTATION: InstrumentationResponse = {
  actions: [
    "document.read",
    "document.write",
    "document.delete",
    "document.share",
    "invoice.approve",
    "invoice.reject",
    "user.login",
    "agent.tool_call",
    "agent.decision",
    "audit.export",
    "agent.run.started",
    "agent.run.completed",
    "pii.access",
    "policy.check",
  ],
  since: daysAgo(30),
};

export const DEMO_EXECUTIONS: Execution[] = [
  {
    id: DEMO_PRIMARY_EXECUTION_ID,
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status: "FAILED",
    root_span_id: "span-root-001",
    trigger: "customer_ticket",
    started_at: hoursAgo(6),
    ended_at: hoursAgo(6),
    metadata: { ticket_id: "TKT-88421", channel: "chat" },
    created_at: hoursAgo(6),
  },
  {
    id: "demo-exec-completed-bal-002",
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status: "COMPLETED",
    root_span_id: "span-root-002",
    trigger: "customer_ticket",
    started_at: hoursAgo(14),
    ended_at: hoursAgo(14),
    metadata: { ticket_id: "TKT-88390", channel: "email" },
    created_at: hoursAgo(14),
  },
  {
    id: "demo-exec-completed-esc-003",
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status: "COMPLETED",
    root_span_id: "span-root-003",
    trigger: "customer_ticket",
    started_at: hoursAgo(28),
    ended_at: hoursAgo(28),
    metadata: { ticket_id: "TKT-88312", channel: "chat", escalated: "true" },
    created_at: hoursAgo(28),
  },
  {
    id: "demo-exec-completed-ref-004",
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status: "COMPLETED",
    root_span_id: "span-root-004",
    trigger: "customer_ticket",
    started_at: daysAgo(2),
    ended_at: daysAgo(2),
    metadata: { ticket_id: "TKT-88104", channel: "chat", refund_usd: "49.99" },
    created_at: daysAgo(2),
  },
  {
    id: "demo-exec-completed-kyc-005",
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status: "COMPLETED",
    root_span_id: "span-root-005",
    trigger: "compliance_review",
    started_at: daysAgo(4),
    ended_at: daysAgo(4),
    metadata: { review_type: "kyc_refresh" },
    created_at: daysAgo(4),
  },
  {
    id: "demo-exec-completed-fraud-006",
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status: "COMPLETED",
    root_span_id: "span-root-006",
    trigger: "fraud_signal",
    started_at: daysAgo(5),
    ended_at: daysAgo(5),
    metadata: { signal: "velocity_check" },
    created_at: daysAgo(5),
  },
];

const FAILED_SPANS: Span[] = [
  {
    id: "span-root-001",
    execution_id: DEMO_PRIMARY_EXECUTION_ID,
    span_type: "CUSTOM",
    name: "run.started",
    status: "COMPLETED",
    started_at: hoursAgo(6),
    ended_at: hoursAgo(6),
    metadata: { ticket: "TKT-88421" },
  },
  {
    id: "span-llm-001",
    execution_id: DEMO_PRIMARY_EXECUTION_ID,
    parent_span_id: "span-root-001",
    span_type: "MODEL_INVOCATION",
    name: "llm.triage",
    status: "COMPLETED",
    started_at: hoursAgo(6),
    ended_at: hoursAgo(6),
    metadata: {},
    model_invocation: {
      model_name: "claude-sonnet-4-5",
      model_provider: "anthropic",
      prompt_tokens: 842,
      completion_tokens: 156,
      total_tokens: 998,
      latency_ms: 1240,
      temperature: 0.2,
      cost_usd: 0.00487,
      session_id: "sess_01HZF1A1A1",
      prompt: {
        inline: {
          messages: [
            {
              role: "system",
              content:
                "You are the Acme Finance support copilot. Triage the customer ticket, decide whether it can be resolved automatically, and call tools to look up account state before answering. Never reveal internal account flags.",
            },
            {
              role: "user",
              content:
                "Ticket TKT-88421: My card payment of $1,240.55 failed this morning but the money left my account. Can you check what happened and refund me?",
            },
          ],
        },
        size_bytes: 512,
        content_type: "application/json",
      },
      completion: {
        inline: {
          messages: [
            {
              role: "assistant",
              content:
                "This looks like a failed capture with a pending authorization hold. I'll look up the account and the payment intent to confirm before initiating a refund.",
              tool_calls: [
                { name: "lookup_account", args: { customer_id: "cus_9k2m" } },
              ],
            },
          ],
        },
        size_bytes: 318,
        content_type: "application/json",
      },
    },
  },
  {
    id: "span-tool-001",
    execution_id: DEMO_PRIMARY_EXECUTION_ID,
    parent_span_id: "span-llm-001",
    span_type: "TOOL_CALL",
    name: "tool.lookup_account",
    status: "COMPLETED",
    started_at: hoursAgo(6),
    ended_at: hoursAgo(6),
    metadata: { account_id: "acct_7f2a" },
    tool_call: {
      tool_name: "lookup_account",
      duration_ms: 320,
      input: { inline: { customer_id: "cus_9k2m" }, size_bytes: 48 },
      output: { inline: { balance_usd: 1240.55, tier: "premium" }, size_bytes: 64 },
    },
  },
  {
    id: "span-tool-002",
    execution_id: DEMO_PRIMARY_EXECUTION_ID,
    parent_span_id: "span-llm-001",
    span_type: "TOOL_CALL",
    name: "tool.process_payment",
    status: "FAILED",
    started_at: hoursAgo(6),
    ended_at: hoursAgo(6),
    metadata: { gateway: "stripe" },
    tool_call: {
      tool_name: "process_payment",
      duration_ms: 5021,
      input: { inline: { amount_usd: 49.99, idempotency_key: "idem_88421" }, size_bytes: 72 },
    },
    error: {
      code: "GATEWAY_TIMEOUT",
      message: "Payment gateway did not respond within 5s (Stripe connect timeout)",
    },
  },
];

export const DEMO_SPANS: Record<string, Span[]> = {
  [DEMO_PRIMARY_EXECUTION_ID]: FAILED_SPANS,
  "demo-exec-completed-bal-002": [
    {
      id: "span-root-002",
      execution_id: "demo-exec-completed-bal-002",
      span_type: "CUSTOM",
      name: "run.started",
      status: "COMPLETED",
      started_at: hoursAgo(14),
      ended_at: hoursAgo(14),
      metadata: {},
    },
    {
      id: "span-tool-bal",
      execution_id: "demo-exec-completed-bal-002",
      parent_span_id: "span-root-002",
      span_type: "TOOL_CALL",
      name: "tool.lookup_account",
      status: "COMPLETED",
      started_at: hoursAgo(14),
      ended_at: hoursAgo(14),
      metadata: {},
      tool_call: { tool_name: "lookup_account", duration_ms: 210 },
    },
  ],
};

export const DEMO_DAG: Record<string, { nodes: DAGNode[]; edges: DAGEdge[] }> = {
  [DEMO_PRIMARY_EXECUTION_ID]: {
    nodes: FAILED_SPANS.map((s) => ({
      id: s.id,
      span_id: s.id,
      name: s.name,
      span_type: s.span_type,
      status: s.status,
      started_at: s.started_at,
      ended_at: s.ended_at,
      duration_ms: 800,
      metadata: s.metadata,
    })),
    edges: [
      { source: "span-root-001", target: "span-llm-001", edge_type: "parent_child" },
      { source: "span-llm-001", target: "span-tool-001", edge_type: "parent_child" },
      { source: "span-llm-001", target: "span-tool-002", edge_type: "parent_child" },
    ],
  },
};

export const DEMO_REPLAY: Record<string, ReplayResponse> = {
  [DEMO_PRIMARY_EXECUTION_ID]: {
    execution: DEMO_EXECUTIONS[0],
    total_frames: 4,
    frames: [
      {
        sequence_number: 1,
        event_type: "run.started",
        span_id: "span-root-001",
        span_name: "run.started",
        span_type: "CUSTOM",
        delta_ms: 0,
        elapsed_ms: 0,
        event: {
          id: "evt-1",
          execution_id: DEMO_PRIMARY_EXECUTION_ID,
          span_id: "span-root-001",
          event_type: "run.started",
          timestamp: hoursAgo(6),
          sequence_number: 1,
          metadata: { ticket_id: "TKT-88421" },
        },
      },
      {
        sequence_number: 2,
        event_type: "llm.completed",
        span_id: "span-llm-001",
        span_name: "llm.triage",
        span_type: "MODEL_INVOCATION",
        delta_ms: 1240,
        elapsed_ms: 1240,
        event: {
          id: "evt-2",
          execution_id: DEMO_PRIMARY_EXECUTION_ID,
          span_id: "span-llm-001",
          event_type: "llm.completed",
          timestamp: hoursAgo(6),
          sequence_number: 2,
          metadata: { intent: "billing_dispute" },
        },
      },
      {
        sequence_number: 3,
        event_type: "tool.completed",
        span_id: "span-tool-001",
        span_name: "tool.lookup_account",
        span_type: "TOOL_CALL",
        delta_ms: 320,
        elapsed_ms: 1560,
        event: {
          id: "evt-3",
          execution_id: DEMO_PRIMARY_EXECUTION_ID,
          span_id: "span-tool-001",
          event_type: "tool.completed",
          timestamp: hoursAgo(6),
          sequence_number: 3,
          metadata: {},
        },
      },
      {
        sequence_number: 4,
        event_type: "tool.failed",
        span_id: "span-tool-002",
        span_name: "tool.process_payment",
        span_type: "TOOL_CALL",
        delta_ms: 5021,
        elapsed_ms: 6581,
        event: {
          id: "evt-4",
          execution_id: DEMO_PRIMARY_EXECUTION_ID,
          span_id: "span-tool-002",
          event_type: "tool.failed",
          timestamp: hoursAgo(6),
          sequence_number: 4,
          metadata: { error: "GATEWAY_TIMEOUT" },
        },
      },
    ],
  },
};

function makeAuditEvent(
  seq: number,
  action: string,
  actor: AuditEvent["actor"],
  resource: AuditEvent["resource"],
  outcome: AuditEvent["outcome"],
  ts: string,
  metadata?: Record<string, unknown>,
): AuditEvent {
  const prev = seq === 1 ? "0".repeat(64) : `hash_${String(seq - 1).padStart(4, "0")}${"a".repeat(56)}`;
  const hash = `hash_${String(seq).padStart(4, "0")}${"b".repeat(56)}`;
  return {
    id: `evt_demo_${String(seq).padStart(4, "0")}`,
    tenant_id: DEMO_TENANT_ID,
    timestamp: ts,
    actor,
    action,
    resource,
    outcome,
    metadata,
    prev_hash: prev,
    hash,
    sequence_number: seq,
  };
}

const minutesAgo = (m: number) => new Date(now - m * 60_000).toISOString();

const DEMO_AUDIT_ACTIONS = [
  "agent.run.started",
  "agent.run.completed",
  "agent.run.failed",
  "tool.lookup_account",
  "tool.process_refund",
  "tool.process_payment",
  "tool.escalate_human",
  "llm.generate",
  "pii.access",
  "policy.check",
  "audit.export.pdf",
  "human.override",
] as const;

const DEMO_HUMAN_ACTORS = [
  { id: "jordan@acme-finance.com", email: "jordan@acme-finance.com" },
  { id: "sam.rivera@acmefinance.com", email: "sam.rivera@acmefinance.com" },
] as const;

function generateDemoAuditEvent(seq: number): AuditEvent {
  // Narrative anchors at the chain tip (support copilot story).
  if (seq === DEMO_CHAIN_HEIGHT) {
    return makeAuditEvent(
      seq,
      "agent.run.failed",
      { id: "support-copilot", type: "agent" },
      { id: DEMO_PRIMARY_EXECUTION_ID, type: "agent.run", name: "Payment dispute TKT-88421" },
      "failure",
      minutesAgo(18),
      { error: "GATEWAY_TIMEOUT", ticket_id: "TKT-88421" },
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 1) {
    return makeAuditEvent(
      seq,
      "pii.access",
      { id: "support-copilot", type: "agent" },
      { id: "acct_7f2a", type: "account", name: "Customer acct_7f2a" },
      "success",
      minutesAgo(19),
      { fields: ["email", "balance"], reason: "support_inquiry" },
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 2) {
    return makeAuditEvent(
      seq,
      "agent.run.started",
      { id: "support-copilot", type: "agent" },
      { id: DEMO_PRIMARY_EXECUTION_ID, type: "agent.run", name: "Payment dispute TKT-88421" },
      "success",
      minutesAgo(22),
      { channel: "chat" },
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 3) {
    return makeAuditEvent(
      seq,
      "policy.check",
      { id: "support-copilot", type: "agent" },
      { id: "pol_refund_limit", type: "policy", name: "Refund limit policy" },
      "success",
      minutesAgo(34),
      { result: "allowed", max_usd: 500 },
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 4) {
    return makeAuditEvent(
      seq,
      "agent.run.completed",
      { id: "support-copilot", type: "agent" },
      { id: "demo-exec-completed-bal-002", type: "agent.run", name: "Balance inquiry TKT-88390" },
      "success",
      minutesAgo(41),
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 5) {
    return makeAuditEvent(
      seq,
      "tool.escalate_human",
      { id: "support-copilot", type: "agent" },
      { id: "demo-exec-completed-esc-003", type: "agent.run", name: "Escalation TKT-88312" },
      "success",
      minutesAgo(52),
      { queue: "tier2_billing", reason: "regulatory_complaint" },
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 6) {
    return makeAuditEvent(
      seq,
      "audit.export.pdf",
      { id: "jordan@acme-finance.com", type: "human", email: "jordan@acme-finance.com" },
      { id: "export_q2", type: "audit_export", name: "Q2 compliance export" },
      "success",
      daysAgo(3),
      { pages: 42 },
    );
  }
  if (seq === DEMO_CHAIN_HEIGHT - 7) {
    return makeAuditEvent(
      seq,
      "agent.run.completed",
      { id: "support-copilot", type: "agent" },
      { id: "demo-exec-completed-ref-004", type: "agent.run", name: "Refund TKT-88104" },
      "success",
      daysAgo(2),
      { refund_usd: 49.99 },
    );
  }

  const action = DEMO_AUDIT_ACTIONS[(seq * 13) % DEMO_AUDIT_ACTIONS.length];
  const isHuman = seq % 47 === 0;
  const human = DEMO_HUMAN_ACTORS[seq % DEMO_HUMAN_ACTORS.length];
  const actor: AuditEvent["actor"] = isHuman
    ? { id: human.id, type: "human", email: human.email }
    : { id: "support-copilot", type: "agent" };

  const execIdx = seq % DEMO_TOTAL_EXECUTIONS;
  const resourceId =
    execIdx < DEMO_EXECUTIONS.length
      ? DEMO_EXECUTIONS[execIdx].id
      : `demo-exec-${String(execIdx).padStart(5, "0")}`;

  const outcome: AuditEvent["outcome"] =
    action.includes("failed") || (seq % 113 === 0 && !isHuman)
      ? "failure"
      : seq % 29 === 0
        ? "error"
        : "success";

  const ageMinutes = ((DEMO_CHAIN_HEIGHT - seq) / DEMO_CHAIN_HEIGHT) * 30 * 24 * 60;

  return makeAuditEvent(
    seq,
    action,
    actor,
    {
      id: resourceId,
      type: action.startsWith("audit.") ? "audit_export" : "agent.run",
      name: `TKT-${88000 + (seq % 900)}`,
    },
    outcome,
    minutesAgo(Math.max(1, ageMinutes)),
  );
}

/** Newest events -stable snapshot for live stream / share preview. */
export const DEMO_LIVE_AUDIT_EVENTS: AuditEvent[] = Array.from({ length: 50 }, (_, i) =>
  generateDemoAuditEvent(DEMO_CHAIN_HEIGHT - i),
);

/** @deprecated Use DEMO_LIVE_AUDIT_EVENTS or filterDemoAuditEvents. */
export const DEMO_AUDIT_EVENTS = DEMO_LIVE_AUDIT_EVENTS;

/** Stable live-stream snapshot for demo mode (no SSE). */
export const DEMO_STREAM_SNAPSHOT = {
  connected: true,
  everOpened: true,
  gaveUp: false,
  events: DEMO_LIVE_AUDIT_EVENTS,
  lastReceivedAt: now,
};

export const DEMO_VERIFY_RESULT: VerifyResult = {
  valid: true,
  tenant_id: DEMO_TENANT_ID,
  events_checked: DEMO_CHAIN_HEIGHT,
  broken_count: 0,
  root_hash: "a3f9c2e1b8d7046f5a2c9e0b7d3f1a8c6e4b2d0f9a7c5e3b1d8f6a4c2e0b9d7",
  from: daysAgo(30),
  to: new Date().toISOString(),
};

function auditEventMatches(
  event: AuditEvent,
  filter: { action?: string; actor_id?: string; resource_id?: string; outcome?: string },
): boolean {
  if (filter.action && !event.action.includes(filter.action)) return false;
  if (filter.actor_id && event.actor.id !== filter.actor_id) return false;
  if (filter.resource_id && event.resource.id !== filter.resource_id) return false;
  if (filter.outcome && event.outcome !== filter.outcome) return false;
  return true;
}

export function getDemoAuditEvent(id: string): AuditEvent | undefined {
  const match = id.match(/^evt_demo_(\d+)$/);
  if (!match) return undefined;
  const seq = parseInt(match[1], 10);
  if (seq < 1 || seq > DEMO_CHAIN_HEIGHT) return undefined;
  return generateDemoAuditEvent(seq);
}

export function filterDemoAuditEvents(filter: {
  page?: number;
  page_size?: number;
  action?: string;
  actor_id?: string;
  resource_id?: string;
  outcome?: string;
}): { events: AuditEvent[]; total: number; page: number; page_size: number } {
  const page = filter.page ?? 1;
  const pageSize = filter.page_size ?? 50;
  const skip = (page - 1) * pageSize;

  let total = 0;
  const events: AuditEvent[] = [];

  for (let seq = DEMO_CHAIN_HEIGHT; seq >= 1; seq--) {
    const event = generateDemoAuditEvent(seq);
    if (!auditEventMatches(event, filter)) continue;
    if (total >= skip && events.length < pageSize) {
      events.push(event);
    }
    total++;
  }

  return { events, total, page, page_size: pageSize };
}

function generateDemoExecution(index: number): Execution {
  const minutes = index < 40 ? index * 1.25 : 60 + (index - 40) * 14;
  const startedAt = minutesAgo(minutes);

  if (index < DEMO_EXECUTIONS.length) {
    const base = DEMO_EXECUTIONS[index];
    if (base.status === "RUNNING") {
      return { ...base, started_at: startedAt, created_at: startedAt, ended_at: undefined };
    }
    const durationMs =
      base.id === DEMO_PRIMARY_EXECUTION_ID
        ? 6581
        : base.status === "FAILED"
          ? 5200
          : 1400 + (index % 5) * 320;
    const endedAt = new Date(new Date(startedAt).getTime() + durationMs).toISOString();
    return { ...base, started_at: startedAt, ended_at: endedAt, created_at: startedAt };
  }

  const statuses = ["COMPLETED", "COMPLETED", "COMPLETED", "FAILED", "RUNNING"] as const;
  const status = statuses[index % statuses.length];
  const ticket = 88000 + (index % 900);
  const durationMs = status === "RUNNING" ? 0 : 1800 + (index % 7) * 420;

  return {
    id: `demo-exec-${String(index).padStart(5, "0")}`,
    agent_id: "support-copilot",
    agent_name: "Support Copilot",
    status,
    root_span_id: `span-root-${String(index).padStart(5, "0")}`,
    trigger: index % 11 === 0 ? "compliance_review" : "customer_ticket",
    started_at: startedAt,
    ended_at:
      status === "RUNNING"
        ? undefined
        : new Date(new Date(startedAt).getTime() + durationMs).toISOString(),
    metadata: { ticket_id: `TKT-${ticket}`, channel: index % 2 === 0 ? "chat" : "email" },
    created_at: startedAt,
  };
}

function offsetIso(iso: string, deltaMs: number): string {
  return new Date(new Date(iso).getTime() + deltaMs).toISOString();
}

function relinkHandSpans(spans: Span[], exec: Execution): Span[] {
  const startMs = new Date(exec.started_at).getTime();
  let cursor = 0;
  return spans.map((span) => {
    const spanDur =
      span.tool_call?.duration_ms ??
      span.model_invocation?.latency_ms ??
      400;
    const started_at = new Date(startMs + cursor).toISOString();
    cursor += 10;
    const ended_at = new Date(startMs + cursor + spanDur).toISOString();
    cursor += spanDur;
    return {
      ...span,
      execution_id: exec.id,
      started_at,
      ended_at: exec.status === "RUNNING" ? undefined : ended_at,
    };
  });
}

function buildDagFromSpans(spans: Span[]): { nodes: DAGNode[]; edges: DAGEdge[] } {
  const nodes: DAGNode[] = spans.map((s) => ({
    id: s.id,
    span_id: s.id,
    name: s.name,
    span_type: s.span_type,
    status: s.status,
    started_at: s.started_at,
    ended_at: s.ended_at,
    duration_ms:
      s.ended_at != null
        ? Math.max(1, new Date(s.ended_at).getTime() - new Date(s.started_at).getTime())
        : 100,
    metadata: s.metadata ?? {},
  }));
  const edges: DAGEdge[] = spans
    .filter((s) => s.parent_span_id)
    .map((s) => ({
      source: s.parent_span_id!,
      target: s.id,
      edge_type: "parent_child" as const,
    }));
  return { nodes, edges };
}

function generateDemoSpans(exec: Execution): Span[] {
  const handcrafted = DEMO_SPANS[exec.id];
  if (handcrafted?.length) return relinkHandSpans(handcrafted, exec);

  const start = exec.started_at;
  const rootId = exec.root_span_id;
  const failed = exec.status === "FAILED";
  const running = exec.status === "RUNNING";

  const llmDuration = 840 + (exec.id.length % 6) * 110;
  const lookupDuration = 210 + (exec.id.charCodeAt(0) % 5) * 60;
  const tailDuration = failed ? 5021 : 340;

  const rootStarted = start;
  const llmStarted = offsetIso(rootStarted, 10);
  const llmEnded = offsetIso(llmStarted, llmDuration);
  const lookupStarted = offsetIso(llmEnded, 10);
  const lookupEnded = offsetIso(lookupStarted, lookupDuration);
  const tailStarted = offsetIso(lookupEnded, 10);
  const tailEnded = offsetIso(tailStarted, tailDuration);

  const root: Span = {
    id: rootId,
    execution_id: exec.id,
    span_type: "CUSTOM",
    name: "run.started",
    status: running ? "STARTED" : "COMPLETED",
    started_at: rootStarted,
    ended_at: running ? undefined : exec.ended_at ?? tailEnded,
    metadata: exec.metadata ?? {},
  };

  const llm: Span = {
    id: `${rootId}-llm`,
    execution_id: exec.id,
    parent_span_id: rootId,
    span_type: "MODEL_INVOCATION",
    name: "llm.triage",
    status: "COMPLETED",
    started_at: llmStarted,
    ended_at: llmEnded,
    metadata: {},
    model_invocation: {
      model_name: "claude-sonnet-4-5",
      model_provider: "anthropic",
      prompt_tokens: 640 + (exec.id.length * 11) % 380,
      completion_tokens: 148,
      total_tokens: 788,
      latency_ms: llmDuration,
      temperature: 0.2,
      cost_usd: 0.0031,
      prompt: {
        inline: {
          messages: [
            {
              role: "system",
              content:
                "You are the Acme Finance support copilot. Triage the ticket and call tools to verify account state before answering.",
            },
            {
              role: "user",
              content: `Customer ticket ${exec.metadata?.ticket ?? "TKT-00000"}: please review my recent account activity and advise.`,
            },
          ],
        },
        size_bytes: 296,
        content_type: "application/json",
      },
      completion: {
        inline: {
          messages: [
            {
              role: "assistant",
              content:
                "I'll verify the account before responding.",
              tool_calls: [{ name: "lookup_account", args: { customer_id: "cus_9k2m" } }],
            },
          ],
        },
        size_bytes: 142,
        content_type: "application/json",
      },
    },
  };

  const lookup: Span = {
    id: `${rootId}-lookup`,
    execution_id: exec.id,
    parent_span_id: `${rootId}-llm`,
    span_type: "TOOL_CALL",
    name: "tool.lookup_account",
    status: "COMPLETED",
    started_at: lookupStarted,
    ended_at: lookupEnded,
    metadata: {},
    tool_call: { tool_name: "lookup_account", duration_ms: lookupDuration },
  };

  const tail: Span = failed
    ? {
      id: `${rootId}-payment`,
      execution_id: exec.id,
      parent_span_id: `${rootId}-llm`,
      span_type: "TOOL_CALL",
      name: "tool.process_payment",
      status: "FAILED",
      started_at: tailStarted,
      ended_at: tailEnded,
      metadata: { gateway: "stripe" },
      tool_call: { tool_name: "process_payment", duration_ms: tailDuration },
      error: {
        code: "GATEWAY_TIMEOUT",
        message: "Payment gateway did not respond within 5s",
      },
    }
    : exec.metadata?.escalated === "true"
      ? {
        id: `${rootId}-escalate`,
        execution_id: exec.id,
        parent_span_id: `${rootId}-llm`,
        span_type: "TOOL_CALL",
        name: "tool.escalate_human",
        status: "COMPLETED",
        started_at: tailStarted,
        ended_at: tailEnded,
        metadata: { queue: "tier2_billing" },
        tool_call: { tool_name: "escalate_human", duration_ms: tailDuration },
      }
      : {
        id: `${rootId}-complete`,
        execution_id: exec.id,
        parent_span_id: `${rootId}-llm`,
        span_type: "TOOL_CALL",
        name: "tool.complete_ticket",
        status: "COMPLETED",
        started_at: tailStarted,
        ended_at: tailEnded,
        metadata: {},
        tool_call: { tool_name: "complete_ticket", duration_ms: tailDuration },
      };

  return [root, llm, lookup, tail];
}

export function getDemoSpans(executionId: string): Span[] {
  const exec = getDemoExecution(executionId);
  if (!exec) return [];
  return generateDemoSpans(exec);
}

export function getDemoDAG(executionId: string): { nodes: DAGNode[]; edges: DAGEdge[] } {
  const exec = getDemoExecution(executionId);
  if (!exec) return { nodes: [], edges: [] };
  return buildDagFromSpans(getDemoSpans(executionId));
}

function generateDemoReplay(exec: Execution, spans: Span[]): ReplayResponse {
  const hand = DEMO_REPLAY[exec.id];
  if (hand) return { ...hand, execution: exec };

  let elapsed = 0;
  let seq = 0;
  const frames: ReplayFrame[] = [];

  for (const span of spans) {
    const delta =
      span.tool_call?.duration_ms ?? span.model_invocation?.latency_ms ?? 180;
    seq += 1;
    frames.push({
      sequence_number: seq,
      event_type: "SPAN_STARTED",
      span_id: span.id,
      span_name: span.name,
      span_type: span.span_type,
      delta_ms: frames.length === 0 ? 0 : delta,
      elapsed_ms: elapsed,
      event: {
        id: `replay-${exec.id}-${seq}`,
        execution_id: exec.id,
        span_id: span.id,
        event_type: "span.started",
        timestamp: span.started_at,
        sequence_number: seq,
        metadata: {},
      },
    });
    elapsed += delta;
    seq += 1;
    const endedAt = span.ended_at ?? offsetIso(span.started_at, delta);
    frames.push({
      sequence_number: seq,
      event_type: span.status === "FAILED" ? "SPAN_FAILED" : "SPAN_ENDED",
      span_id: span.id,
      span_name: span.name,
      span_type: span.span_type,
      delta_ms: delta,
      elapsed_ms: elapsed,
      event: {
        id: `replay-${exec.id}-${seq}`,
        execution_id: exec.id,
        span_id: span.id,
        event_type: span.status === "FAILED" ? "span.failed" : "span.ended",
        timestamp: endedAt,
        sequence_number: seq,
        metadata: {},
      },
    });
  }

  return { execution: exec, frames, total_frames: frames.length };
}

export function getDemoReplay(executionId: string): ReplayResponse {
  const exec = getDemoExecution(executionId);
  if (!exec) {
    return { execution: {} as Execution, frames: [], total_frames: 0 };
  }
  return generateDemoReplay(exec, getDemoSpans(executionId));
}

export function findDemoSpan(spanId: string): Span | undefined {
  const scanLimit = Math.min(DEMO_TOTAL_EXECUTIONS, 200);
  for (let i = 0; i < scanLimit; i++) {
    const exec = generateDemoExecution(i);
    const found = getDemoSpans(exec.id).find((s) => s.id === spanId);
    if (found) return found;
  }
  return undefined;
}

export function getDemoExecution(id: string): Execution | undefined {
  const idx = DEMO_EXECUTIONS.findIndex((e) => e.id === id);
  if (idx >= 0) return generateDemoExecution(idx);

  const match = id.match(/^demo-exec-(\d+)$/);
  if (match) return generateDemoExecution(parseInt(match[1], 10));
  return undefined;
}

export function listDemoExecutions(params?: {
  page_size?: number;
  offset?: number;
  status?: string;
  agent_id?: string;
}): { executions: Execution[]; total: number } {
  const offset = params?.offset ?? 0;
  const pageSize = params?.page_size ?? 50;

  let total = 0;
  const executions: Execution[] = [];

  for (let i = 0; i < DEMO_TOTAL_EXECUTIONS; i++) {
    const exec = generateDemoExecution(i);
    if (params?.status && exec.status !== params.status) continue;
    if (
      params?.agent_id &&
      !exec.agent_id.toLowerCase().includes(params.agent_id.toLowerCase())
    ) {
      continue;
    }
    if (total >= offset && executions.length < pageSize) {
      executions.push(exec);
    }
    total++;
  }

  return { executions, total };
}

export function demoEvidencePackBlob(): Blob {
  const pack = {
    tenant_id: DEMO_TENANT_ID,
    workspace: DEMO_WORKSPACE_NAME,
    generated_at: new Date().toISOString(),
    events: DEMO_LIVE_AUDIT_EVENTS,
    verify: DEMO_VERIFY_RESULT,
    total_events: DEMO_CHAIN_HEIGHT,
  };
  return new Blob([JSON.stringify(pack, null, 2)], { type: "application/json" });
}

export function demoPdfBlob(): Blob {
  const lines = [
    "Fact0 Audit Report (Demo)",
    "Workspace: Acme Finance -Support Copilot",
    `Events in chain: ${DEMO_CHAIN_HEIGHT.toLocaleString()}`,
    "Chain valid: yes",
    "",
    "Recent actions (sample):",
    ...DEMO_LIVE_AUDIT_EVENTS.slice(0, 12).map(
      (e) => `- ${e.timestamp} ${e.action} (${e.outcome}) by ${e.actor.id}`,
    ),
  ];
  return new Blob([lines.join("\n")], { type: "application/pdf" });
}
