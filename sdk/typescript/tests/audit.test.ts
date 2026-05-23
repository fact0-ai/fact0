import { afterEach, describe, expect, it, vi } from "vitest";
import { AuditClient } from "../src/audit/client.js";
import { createHttpClient } from "../src/http.js";
import { installMockFetch, type RecordedRequest } from "./helpers.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

function auditClient() {
  const requests: RecordedRequest[] = [];
  installMockFetch((req) => {
    requests.push(req);
    return { status: 200, body: { accepted: 1 } };
  });
  const http = createHttpClient({ apiKey: "alk_live_test", baseUrl: "http://localhost:8000", maxRetries: 0 });
  return { client: new AuditClient(http), requests };
}

describe("AuditClient", () => {
  it("logs a single event via the batch endpoint", async () => {
    const { client, requests } = auditClient();
    await client.log({
      actor: { id: "user_1", type: "human" },
      action: "document.read",
      resource: { id: "doc_1", type: "document" },
      outcome: "success",
    });

    expect(requests[0]?.method).toBe("POST");
    expect(requests[0]?.url).toContain("/v1/events/batch");
    expect(requests[0]?.body).toEqual({
      events: [
        {
          actor: { id: "user_1", type: "human" },
          action: "document.read",
          resource: { id: "doc_1", type: "document" },
          outcome: "success",
        },
      ],
    });
  });

  it("lists events with query params", async () => {
    const { client, requests } = auditClient();
    await client.listEvents({ actor_id: "user_1", page: 2 });

    expect(requests[0]?.method).toBe("GET");
    expect(requests[0]?.url).toContain("actor_id=user_1");
    expect(requests[0]?.url).toContain("page=2");
  });
});
