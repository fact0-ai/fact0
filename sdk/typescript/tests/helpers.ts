import { vi } from "vitest";

export type RecordedRequest = {
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: unknown;
};

export function installMockFetch(
  handler: (req: RecordedRequest) => { status: number; body?: unknown; headers?: Record<string, string> }
) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const headers: Record<string, string> = {};
    if (init?.headers) {
      const raw = new Headers(init.headers as HeadersInit);
      raw.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    let body: unknown;
    if (init?.body) {
      body = JSON.parse(String(init.body));
    }

    const recorded: RecordedRequest = { method, url, headers, body };
    const result = handler(recorded);
    const responseBody =
      result.body === undefined ? "" : typeof result.body === "string" ? result.body : JSON.stringify(result.body);
    return new Response(responseBody, {
      status: result.status,
      headers: result.headers ?? { "Content-Type": "application/json" },
    });
  });

  vi.stubGlobal("fetch", fetchMock);
  return { fetchMock, getLastRequest: () => fetchMock.mock.calls.at(-1) };
}
