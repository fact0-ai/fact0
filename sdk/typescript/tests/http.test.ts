import { describe, expect, it } from "vitest";
import { createHttpClient } from "../src/http.js";

describe("http client", () => {
  it("builds without api key for telemetry", () => {
    const client = createHttpClient({ baseUrl: "http://localhost:8000" });
    expect(client).toBeDefined();
  });
});
