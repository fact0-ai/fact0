import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { runInNewContext } from "node:vm";
import ts from "typescript";
const require = createRequire(import.meta.url);
function load(path, mocks = {}, globals = {}) {
  const source = readFileSync(new URL("../" + path, import.meta.url), "utf8");
  const code = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  const exports = {};
  runInNewContext(
    code,
    {
      exports,
      require: (id) => (id in mocks ? mocks[id] : require(id)),
      process: { env: {} },
      URL,
      URLSearchParams,
      Request,
      Response,
      Headers,
      Date,
      console,
      ...globals,
    },
    { filename: path },
  );
  return exports;
}

const sourceDocs = load("lib/docs-origin.ts");
const sourceBase = "https://github.com/fact0-ai/fact0/blob/main";
for (const path of [undefined, "", "/", "/docs", "/docs/", "quickstart", "/quickstart"])
  assert.equal(sourceDocs.docsHref(path), `${sourceBase}/README.md#quickstart`);
assert.equal(
  sourceDocs.docsHref("/sdk/python/installation"),
  `${sourceBase}/README.md#python`,
);
for (const path of [
  "integrations/claude-code",
  "integrations/claude-code#capture-modes",
  "guides/self-hosting",
  "concepts/executions",
  "observability/prompt-registry",
]) {
  const [page, fragment] = path.split("#");
  assert.equal(
    sourceDocs.docsHref(path),
    `${sourceBase}/docs/${page}.mdx${fragment ? `#${fragment}` : ""}`,
  );
  assert.ok(existsSync(new URL(`../../docs/${page}.mdx`, import.meta.url)));
}
const publishedDocs = load("lib/docs-origin.ts", {}, {
  process: { env: { NEXT_PUBLIC_DOCS_URL: " https://docs.example.invalid/ " } },
});
assert.equal(publishedDocs.docsHref(), "https://docs.example.invalid");
assert.equal(publishedDocs.docsHref("/docs/quickstart"), "https://docs.example.invalid/quickstart");
assert.equal(
  publishedDocs.docsHref("integrations/claude-code#capture-modes"),
  "https://docs.example.invalid/integrations/claude-code#capture-modes",
);
console.log("Documentation links passed: GitHub first-use fallback, checked-in guides, anchors and explicit published-docs override.");

const { sanitiseRedirectPath } = load("lib/app-origin.ts");
for (const path of [
  "//example.com",
  "/\\example.com",
  "https://example.com",
  "/dashboard/../../evil",
  "/dashboard\n/evil",
  "/sign-in",
])
  assert.equal(sanitiseRedirectPath(path), "/dashboard");
assert.equal(
  sanitiseRedirectPath("/dashboard/executions/abc?tab=replay#frame"),
  "/dashboard/executions/abc?tab=replay#frame",
);

const pending = [];
const authClient = {
  token: () =>
    new Promise((resolve, reject) => pending.push({ resolve, reject })),
};
const { fetchToken, invalidateTokenCache } = load("lib/auth-token.ts", {
  "./auth-client": { authClient },
});
const first = fetchToken();
const dedup = fetchToken();
assert.equal(pending.length, 1, "concurrent fetches share a token request");
invalidateTokenCache();
const next = fetchToken();
assert.equal(pending.length, 2);
pending[1].resolve({ data: { token: "new-session" } });
assert.equal(await next, "new-session");
pending[0].resolve({ data: { token: "old-session" } });
assert.equal(
  await first,
  "new-session",
  "a stale response cannot reintroduce the previous session token",
);
assert.equal(await dedup, "new-session");
assert.equal(await fetchToken(), "new-session");
invalidateTokenCache();
const failing = fetchToken();
invalidateTokenCache();
const current = fetchToken();
pending[3].resolve({ data: { token: "current-session" } });
assert.equal(await current, "current-session");
pending[2].reject(new Error("late failure"));
assert.equal(
  await failing,
  "current-session",
  "a late rejected request cannot clear the current token",
);
assert.equal(pending.length, 4);

let forwarded = 0;
const routes = load("app/api/auth/[...all]/route.ts", {
  "@/lib/auth": {
    getAuth: () => ({
      handler: () => {
        forwarded++;
        return new Response("auth");
      },
    }),
  },
});
for (const endpoint of [
  "sign-up/email",
  "organization/create",
  "organization/invite-member",
  "organization/set-active",
  "sign-in/social",
  "request-password-reset",
  "change-email",
]) {
  const response = await routes.POST(
    new Request("http://localhost/api/auth/" + endpoint, { method: "POST" }),
  );
  assert.equal(
    response.status,
    404,
    endpoint + " is unavailable without constructing authentication",
  );
}
assert.equal(forwarded, 0);
assert.equal(
  (
    await routes.POST(
      new Request("http://localhost/api/auth/sign-in/email", {
        method: "POST",
      }),
    )
  ).status,
  200,
);
assert.equal(
  (await routes.GET(new Request("http://localhost/api/auth/jwks"))).status,
  200,
);
assert.equal(forwarded, 2);

const { parseCapturedJSON, stringifyCapturedJSON } = load(
  "lib/captured-json.ts",
);
const payload =
  '{"small":123,"large":900719925474099312345,"decimal":0.1234567890123456789012345,"array":[900719925474099312346]}';
const parsed = parseCapturedJSON(payload);
assert.equal(parsed.small, 123);
const restored = stringifyCapturedJSON(parsed).replace(/\s/g, "");
assert.equal(
  restored,
  payload,
  "numeric payload literals survive parse, render, copy and download",
);
console.log(
  "Core regressions passed: redirect safety, token invalidation races, closed auth routes, lossless captured payloads.",
);

// Verify the real transport contract: protected exports use headers, preserve the
// requested date range, and fail visibly instead of downloading an error body.
const requests = [];
let rejectExport = false;
const { auditClient } = load(
  "lib/audit-api.ts",
  {
    "./captured-json": { parseCapturedJSON },
    "./demo/demo-audit-client": {},
    "./demo/demo-context": {},
    "./use-me": {},
  },
  {
    fetch: async (path, init) => {
      requests.push({
        url: new URL(path, "http://localhost"),
        headers: new Headers(init?.headers),
      });
      if (rejectExport) return new Response("session expired", { status: 401 });
      if (String(path).startsWith("/v1/events"))
        return Response.json({ events: [], total: 0, page: 3, page_size: 25 });
      return new Response(
        String(path).includes("evidence-pack") ? "ZIP bytes" : "PDF bytes",
      );
    },
  },
);
const audit = auditClient(async () => "owner-token");
const from = "2026-09-16T01:00:00.000Z",
  to = "2026-09-16T02:00:00.000Z";
assert.equal(await (await audit.downloadPDF(from, to)).text(), "PDF bytes");
assert.equal(
  await (await audit.downloadEvidencePack(from, to)).text(),
  "ZIP bytes",
);
assert.equal(requests[0].url.pathname, "/v1/export/pdf");
assert.equal(requests[1].url.pathname, "/v1/export/evidence-pack");
for (const request of requests) {
  assert.equal(request.headers.get("Authorization"), "Bearer owner-token");
  assert.equal(request.url.searchParams.get("from"), from);
  assert.equal(request.url.searchParams.get("to"), to);
  assert.equal(request.url.href.includes("owner-token"), false);
}
await audit.listEvents({
  action: "claude_code.*",
  actor_id: "owner",
  actor_type: "agent",
  resource_id: "/a?b",
  session_id: "session-1",
  outcome: "failure",
  from,
  to,
  page: 3,
  page_size: 25,
});
assert.equal(requests[2].url.searchParams.get("resource_id"), "/a?b");
assert.equal(requests[2].url.searchParams.get("session_id"), "session-1");
assert.equal(requests[2].url.searchParams.get("page"), "3");
assert.equal(requests[2].url.searchParams.get("page_size"), "25");
rejectExport = true;
await assert.rejects(() => audit.downloadPDF(from, to), /401: session expired/);
await assert.rejects(
  () => audit.downloadEvidencePack(from, to),
  /401: session expired/,
);
console.log(
  "Audit transport passed: authenticated date-scoped PDF/ZIP exports, surfaced failures, server-side search and pagination.",
);

// A removed fixed route must not fall through to the coding-session dynamic page.
class TestNextResponse extends Response {
  static next() {
    return new TestNextResponse(null);
  }
  static redirect(url) {
    return new TestNextResponse(null, {
      status: 307,
      headers: { Location: String(url) },
    });
  }
}
const { proxy } = load("proxy.ts", {
  "better-auth/cookies": { getSessionCookie: () => "signed-in" },
  "next/server": { NextResponse: TestNextResponse },
  "@/lib/app-origin": { isProtectedPath: () => true },
});
for (const path of [
  "/dashboard/coding-agents/governance",
  "/dashboard/coding-agents/governance/",
  "/dashboard/coding-agents/governance/rules",
  "/dashboard/coding-agents/%67overnance",
]) {
  const url = new URL(path, "http://localhost");
  assert.equal(proxy({ nextUrl: url, url: String(url) }).status, 404);
}
const sessionUrl = new URL(
  "http://localhost/dashboard/coding-agents/session-123",
);
assert.equal(
  proxy({ nextUrl: sessionUrl, url: String(sessionUrl) }).status,
  200,
);
console.log(
  "Retired governance route returns HTTP404 without catching valid session paths.",
);

// Client timestamps can precede server execution creation. Replay's clamped
// elapsed_ms must not erase those real intervals or make waterfall bars negative.
const { buildTraceTimeline, formatTraceDuration } = load(
  "lib/trace-timeline.ts",
);
const timingSpans = [
  {
    id: "parent",
    started_at: "2026-09-16T07:55:31.247340Z",
    ended_at: "2026-09-16T07:55:31.248460Z",
  },
  {
    id: "tool",
    started_at: "2026-09-16T07:55:31.247537Z",
    ended_at: "2026-09-16T07:55:31.247547Z",
  },
];
const timingFrames = [
  {
    sequence_number: 2,
    elapsed_ms: 0,
    delta_ms: 0,
    event: { timestamp: timingSpans[0].ended_at },
  },
  {
    sequence_number: 1,
    elapsed_ms: 0,
    delta_ms: 0,
    event: { timestamp: timingSpans[0].started_at },
  },
];
const timeline = buildTraceTimeline({
  executionStart: "2026-09-16T07:55:31.278336Z",
  executionEnd: "2026-09-16T07:55:31.666286Z",
  reportedDurationMs: 387,
  spans: timingSpans,
  frames: timingFrames,
  now: 0,
});
assert.ok(Math.abs(timeline.totalMs - 418.946) < 0.001);
assert.ok(Math.abs(timeline.beforeExecutionMs - 30.996) < 0.001);
assert.ok(
  timeline.rows.every(
    (row) =>
      row.startMs >= 0 &&
      row.endMs >= row.startMs &&
      row.endMs <= timeline.totalMs,
  ),
);
assert.ok(Math.abs(timeline.rows[0].durationMs - 1.12) < 0.001);
assert.equal(formatTraceDuration(timeline.rows[1].durationMs), "<1ms");
assert.equal(timeline.frames[0].sequence_number, 1);
assert.ok(timeline.frames[1].elapsed_ms > 1);
assert.equal(
  timingFrames[0].elapsed_ms,
  0,
  "raw server timestamps and frames remain unchanged",
);
const emptyReplay = buildTraceTimeline({
  executionStart: "2026-01-01T00:00:00Z",
  executionEnd: "2026-01-01T00:00:00.500Z",
  spans: [],
  frames: [],
  now: 0,
});
assert.equal(
  emptyReplay.totalMs,
  500,
  "an empty replay retains the execution duration",
);
const outside = buildTraceTimeline({
  executionStart: "2026-01-01T00:00:00Z",
  executionEnd: "2026-01-01T00:00:00.500Z",
  spans: [
    {
      id: "late",
      started_at: "2026-01-01T00:00:01Z",
      ended_at: "2026-01-01T00:00:01.500Z",
    },
  ],
  now: 0,
});
assert.equal(outside.totalMs, 1500);
assert.equal(
  outside.rows[0].durationMs,
  500,
  "span bars outside execution bounds retain their duration",
);
console.log(
  "Trace timing passed: earlier client clocks, sub-ms spans, chronological replay, empty replay and late span bounds.",
);
