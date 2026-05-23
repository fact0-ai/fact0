# Fact0 - Explainer

Fact0 is **observability + compliance for AI agents** - like Datadog and a tamper-evident ledger combined into one tool.

---

## The two pipelines

Fact0 has **two parallel pipelines**, each with its own purpose. Understanding the split is the key to understanding the product.

```
┌───────────────────────────────────────────────────────────────┐
│  Audit log  →  "Did it happen? Who? When?"     (compliance)   │
│  Telemetry  →  "How did it happen? Why?"       (debugging)    │
└───────────────────────────────────────────────────────────────┘
```

For high-value events (an LLM call that spent money, an agent that touched
customer data, a refund that was processed) you typically log to **both** -
telemetry for the engineering team, audit for the auditor.

---

## 1. Execution Telemetry - "the X-ray of an agent run"

- **Endpoint:** `POST /api/v1/executions/*`
- **Storage:** `executions`, `spans`, `execution_events`
- **Use for:** debugging, replay, observability

### Span kinds

| `span_type`         | What you log                            | Example                                  |
| ------------------- | --------------------------------------- | ---------------------------------------- |
| `TOOL_CALL`         | External tool / API invocations         | `web_search`, `send_email`, `db.query`   |
| `MODEL_INVOCATION`  | LLM calls                               | `gpt-4o`, `claude-3.5-sonnet`            |
| `STATE_MUTATION`    | Reads / writes to memory, KV, DB, files | `update_user_profile`, `cache.set`       |
| `HUMAN_APPROVAL`    | Human-in-the-loop decisions             | "approve refund $500", manager sign-off  |
| `POLICY_EVALUATION` | Governance / guardrail checks           | OPA rules, PII filters, content policy   |
| `CUSTOM`            | Anything else                           | `plan`, `route_decision`, `branch_taken` |

### Type-specific detail blocks

When you log a span you can attach rich structured data:

- **`ToolCallDetail`** - tool name, version, input/output payloads, duration
- **`ModelInvocationDetail`** - model, provider, prompt, completion, token counts, latency, temperature
- **`StateMutationDetail`** - key, before/after value, mutation type (`create | update | delete`)
- **`HumanApprovalDetail`** - approver ID, decision, reasoning, timestamp
- **`PolicyEvaluationDetail`** - policy ID, result (`allow | deny`), violations list

### Auto-emitted lifecycle events

For every span Fact0 automatically writes:

- `SPAN_STARTED` - span entry
- `SPAN_ENDED` - span exit (success)
- `SPAN_FAILED` - span exit with `FAILED` status

You can also push **custom events mid-span** (token-by-token streaming, retries, intermediate decisions, etc.).

---

## 2. Audit Log - "the compliance ledger"

- **Endpoint:** `POST /v1/events/batch`
- **Storage:** `audit_events` (SHA-256 hash-chained per tenant)
- **Use for:** tamper-evident, regulator-ready proof that something happened

### Schema

```json
{
  "actor":    { "id": "...", "type": "human|agent|system", "email": "..." },
  "action":   "dot.notation.verb",
  "resource": { "id": "...", "type": "...", "name": "..." },
  "outcome":  "success|failure|error",
  "metadata": { "...": "arbitrary" }
}
```

### Common `action` patterns

| Category        | Examples                                                       |
| --------------- | -------------------------------------------------------------- |
| Agent lifecycle | `agent.run.started`, `agent.run.completed`, `agent.run.failed` |
| LLM             | `llm.completion`, `llm.embedding`                              |
| Tools           | `tool.web_search`, `tool.email.sent`, `tool.database.query`    |
| Data            | `data.read`, `data.exported`, `data.deleted`, `pii.redacted`   |
| Decisions       | `decision.branch`, `policy.allow`, `policy.deny`               |
| Human           | `human.approved`, `human.rejected`, `human.override`           |
| Auth / security | `auth.login`, `key.created`, `key.revoked`                     |

---

## Elevator pitch

> Fact0 is **observability + compliance for AI agents** - like Datadog and a tamper-evident ledger combined.
>
> Your agent does five kinds of things: it **calls LLMs**, **invokes tools**, **mutates state**, **asks humans for approval**, and **runs policy checks**. Fact0 records every one of those as a typed span - so you can replay any run frame-by-frame in the dashboard.
>
> In parallel, every action that matters for compliance - who did what to which resource, and whether it succeeded - gets written to a SHA-256 hash-chained audit log. The hash chain means you can prove to a regulator that nothing was deleted or modified after the fact.
>
> So when your agent processes a $10k refund six months from now and someone asks "why did this happen?", you can show them:
>
> 1. The full execution DAG (every model call, every tool, every decision)
> 2. The audit entry proving the human who approved it
> 3. Cryptographic proof neither has been tampered with

---

## End-to-end example: E-commerce refund agent

An agent receives a chat message: *"Please refund order #1234"*

| #   | Action                      | Telemetry span                                                   | Audit event                              |
| --- | --------------------------- | ---------------------------------------------------------------- | ---------------------------------------- |
| 1   | Classify intent             | `MODEL_INVOCATION` `intent_classifier` (gpt-4o, 240 tok, 800 ms) | `agent.run.started`                      |
| 2   | Look up order               | `TOOL_CALL` `orders.get` (input `{id:1234}`)                     | `data.read` on `order:1234`              |
| 3   | Check refund policy         | `POLICY_EVALUATION` `refund_policy` (result `allow`)             | `policy.allow`                           |
| 4   | Ask manager (refund > $500) | `HUMAN_APPROVAL` waiting on `mgr_42`                             | `human.approval_requested`               |
| 5   | Manager clicks Approve      | (span resolves with decision `approve`)                          | `human.approved` actor=`mgr_42@acme.com` |
| 6   | Call Stripe                 | `TOOL_CALL` `stripe.refund` (output `re_abc`)                    | `payment.refunded` outcome=`success`     |
| 7   | Update DB                   | `STATE_MUTATION` `orders.status` before=`paid` after=`refunded`  | `data.updated`                           |
| 8   | Run ends                    | (root span closes)                                               | `agent.run.completed`                    |

### Result

- **Engineer** opens the dashboard → sees the 8-node DAG with timings and can replay the run frame-by-frame.
- **Compliance team** queries the audit log → sees 8 hash-chained entries, exportable to PDF.
- **Six months later** when the customer disputes the refund, you can prove who approved it and replay the exact LLM call that triggered the policy check.

That's the "why Fact0 exists" story in one example.
