export interface HttpConfig {
  apiKey?: string;
  baseUrl?: string;
  sync?: boolean;
  maxRetries?: number;
}

export interface HttpClient {
  request<T>(
    method: string,
    path: string,
    options?: {
      body?: unknown;
      params?: Record<string, string | number | undefined>;
      auth?: boolean;
      expectJson?: boolean;
    }
  ): Promise<T>;
}

const RETRYABLE = new Set([429, 500, 502, 503, 504]);
const USER_AGENT = "@fact0/sdk/1.0.2";

/** Production Fact0 API origin. Override via {@link HttpConfig.baseUrl} for local dev. */
export const DEFAULT_BASE_URL = "https://api.fact0.io";

function resolveBaseUrl(config: HttpConfig): string {
  return (config.baseUrl ?? DEFAULT_BASE_URL).replace(/\/$/, "");
}

function resolveApiKey(config: HttpConfig): string | undefined {
  return config.apiKey ?? process.env.FACT0_API_KEY;
}

export function createHttpClient(config: HttpConfig): HttpClient {
  const baseUrl = resolveBaseUrl(config);
  const apiKey = resolveApiKey(config);
  const sync = config.sync ?? false;
  const maxRetries = config.maxRetries ?? 3;

  return {
    async request<T>(
      method: string,
      path: string,
      options: {
        body?: unknown;
        params?: Record<string, string | number | undefined>;
        auth?: boolean;
        expectJson?: boolean;
      } = {}
    ): Promise<T> {
      const { body, params, auth = true, expectJson = true } = options;
      const url = new URL(baseUrl + path);
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          if (v !== undefined) url.searchParams.set(k, String(v));
        }
      }
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        "User-Agent": USER_AGENT,
      };
      if (auth && apiKey) headers.Authorization = `Bearer ${apiKey}`;
      if (sync) headers["X-Fact0-Sync"] = "true";

      let lastErr: Error | undefined;
      for (let attempt = 0; attempt <= maxRetries; attempt++) {
        const resp = await fetch(url, {
          method,
          headers,
          body: body !== undefined ? JSON.stringify(body) : undefined,
        });
        if (resp.ok) {
          if (!expectJson) return (await resp.arrayBuffer()) as T;
          return (await resp.json()) as T;
        }
        if (!RETRYABLE.has(resp.status)) {
          throw new Error(`${method} ${path} -> ${resp.status}: ${(await resp.text()).slice(0, 200)}`);
        }
        lastErr = new Error(`${method} ${path} -> ${resp.status}`);
        const retryAfter = resp.headers.get("Retry-After");
        if (retryAfter) await sleep(Number(retryAfter) * 1000);
        else if (attempt < maxRetries) await sleep(200 * 2 ** attempt);
      }
      throw lastErr ?? new Error("request failed");
    },
  };
}

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}
