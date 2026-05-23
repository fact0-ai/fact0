import type { HttpClient } from "../http.js";

export class TelemetryClient {
  constructor(private readonly http: HttpClient) {}

  async startExecution(body: {
    agent_id: string;
    agent_name?: string;
    trigger?: string;
    metadata?: Record<string, string>;
    idempotency_key?: string;
  }): Promise<Record<string, unknown>> {
    return this.http.request("POST", "/api/v1/executions", { body });
  }

  async ingestSpans(executionId: string, spans: Record<string, unknown>[]): Promise<Record<string, unknown>> {
    return this.http.request("POST", `/api/v1/executions/${executionId}/spans`, {
      body: { spans },
    });
  }

  async ingestEvents(executionId: string, events: Record<string, unknown>[]): Promise<Record<string, unknown>> {
    return this.http.request("POST", `/api/v1/executions/${executionId}/events`, {
      body: { events },
    });
  }

  async endExecution(executionId: string, status: string): Promise<Record<string, unknown>> {
    return this.http.request("PUT", `/api/v1/executions/${executionId}/end`, {
      body: { status },
    });
  }

  async listExecutions(params: Record<string, string | number | undefined> = {}): Promise<Record<string, unknown>> {
    return this.http.request("GET", "/api/v1/executions", { params });
  }

  async getExecution(id: string): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/api/v1/executions/${id}`);
  }

  async getSpans(executionId: string): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/api/v1/executions/${executionId}/spans`);
  }

  async getDag(executionId: string): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/api/v1/executions/${executionId}/dag`);
  }

  async replay(executionId: string, params: Record<string, number | undefined> = {}): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/api/v1/executions/${executionId}/replay`, { params });
  }
}
