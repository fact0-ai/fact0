import { afterEach, describe, expect, it, vi } from "vitest";
import { createHttpClient } from "../src/http.js";
import { TelemetryClient } from "../src/telemetry/client.js";
import { installMockFetch, type RecordedRequest } from "./helpers.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

function telemetryClient() {
  const requests: RecordedRequest[] = [];
  installMockFetch((req) => {
    requests.push(req);
    return { status: 200, body: { id: "exec_1" } };
  });
  const http = createHttpClient({ apiKey: "alk_live_test", baseUrl: "http://localhost:8000", maxRetries: 0 });
  return { client: new TelemetryClient(http), requests };
}

describe("TelemetryClient", () => {
  it("starts an execution", async () => {
    const { client, requests } = telemetryClient();
    await client.startExecution({ agent_id: "agent_1", agent_name: "demo-agent" });

    expect(requests[0]?.method).toBe("POST");
    expect(requests[0]?.url).toContain("/api/v1/executions");
    expect(requests[0]?.body).toEqual({
      agent_id: "agent_1",
      agent_name: "demo-agent",
    });
  });

  it("ends an execution", async () => {
    const { client, requests } = telemetryClient();
    await client.endExecution("exec_123", "success");

    expect(requests[0]?.method).toBe("PUT");
    expect(requests[0]?.url).toContain("/api/v1/executions/exec_123/end");
    expect(requests[0]?.body).toEqual({ status: "success" });
  });
});
