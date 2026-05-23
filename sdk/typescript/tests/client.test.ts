import { afterEach, describe, expect, it, vi } from "vitest";
import { Fact0Client } from "../src/index.js";
import { installMockFetch } from "./helpers.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Fact0Client", () => {
  it("exposes audit and telemetry modules", () => {
    installMockFetch(() => ({ status: 200, body: {} }));
    const client = new Fact0Client({ apiKey: "alk_live_test", baseUrl: "http://localhost:8000" });
    expect(client.audit).toBeDefined();
    expect(client.telemetry).toBeDefined();
  });
});
