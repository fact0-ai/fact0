import type { HttpClient } from "../http.js";
import type { AuditEventInput, ListEventsParams } from "./types.js";

export class AuditClient {
  constructor(private readonly http: HttpClient) {}

  async log(event: AuditEventInput): Promise<void> {
    await this.http.request("POST", "/v1/events/batch", {
      body: { events: [event] },
    });
  }

  async logBatch(events: AuditEventInput[]): Promise<Record<string, unknown>> {
    return this.http.request("POST", "/v1/events/batch", { body: { events } });
  }

  async getEvent(id: string): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/v1/events/${id}`);
  }

  async listEvents(params: ListEventsParams = {}): Promise<Record<string, unknown>> {
    return this.http.request("GET", "/v1/events", {
      params: params as Record<string, string | number | undefined>,
    });
  }

  async getReceipt(id: string): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/v1/receipts/${id}`);
  }

  async verify(params: { from?: string; to?: string; scan_all?: boolean } = {}): Promise<Record<string, unknown>> {
    return this.http.request("GET", "/v1/verify", {
      params: {
        from: params.from,
        to: params.to,
        scan_all: params.scan_all ? "true" : undefined,
      },
    });
  }

  async verifyEvent(id: string): Promise<Record<string, unknown>> {
    return this.http.request("GET", `/v1/events/${id}/verify`);
  }

  async exportPdf(params: { from?: string; to?: string } = {}): Promise<ArrayBuffer> {
    return this.http.request("GET", "/v1/export/pdf", { params, expectJson: false });
  }

  async exportEvidencePack(params: { from?: string; to?: string } = {}): Promise<ArrayBuffer> {
    return this.http.request("GET", "/v1/export/evidence-pack", { params, expectJson: false });
  }
}
