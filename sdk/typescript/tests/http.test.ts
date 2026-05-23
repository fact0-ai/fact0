import { afterEach, describe, expect, it, vi } from "vitest";
import { createHttpClient } from "../src/http.js";
import { installMockFetch } from "./helpers.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("createHttpClient", () => {
  it("uses default base URL and sends auth header", async () => {
    const requests: Array<{ method: string; url: string; headers: Record<string, string> }> = [];
    installMockFetch((req) => {
      requests.push(req);
      return { status: 200, body: { ok: true } };
    });

    const client = createHttpClient({ apiKey: "alk_live_test", baseUrl: "http://localhost:8000" });
    const result = await client.request("GET", "/v1/events");

    expect(result).toEqual({ ok: true });
    expect(requests[0]?.headers.authorization).toBe("Bearer alk_live_test");
    expect(requests[0]?.url).toBe("http://localhost:8000/v1/events");
  });

  it("adds sync ingest header when enabled", async () => {
    const requests: Array<{ headers: Record<string, string> }> = [];
    installMockFetch((req) => {
      requests.push(req);
      return { status: 200, body: {} };
    });

    const client = createHttpClient({
      apiKey: "alk_live_test",
      baseUrl: "http://localhost:8000",
      sync: true,
    });
    await client.request("POST", "/v1/events/batch", { body: { events: [] } });

    expect(requests[0]?.headers["x-fact0-sync"]).toBe("true");
  });

  it("retries retryable status codes", async () => {
    let calls = 0;
    installMockFetch(() => {
      calls += 1;
      if (calls === 1) {
        return { status: 503, body: { error: "unavailable" } };
      }
      return { status: 200, body: { ok: true } };
    });

    const client = createHttpClient({
      apiKey: "alk_live_test",
      baseUrl: "http://localhost:8000",
      maxRetries: 2,
    });
    const result = await client.request("GET", "/v1/events");

    expect(result).toEqual({ ok: true });
    expect(calls).toBe(2);
  });

  it("throws on non-retryable client errors", async () => {
    installMockFetch(() => ({ status: 400, body: "bad request" }));

    const client = createHttpClient({
      apiKey: "alk_live_test",
      baseUrl: "http://localhost:8000",
      maxRetries: 0,
    });

    await expect(client.request("GET", "/v1/events")).rejects.toThrow("400");
  });
});
