import {
  createHttpClient,
  DEFAULT_BASE_URL,
  type HttpClient,
  type HttpConfig,
} from "./http.js";
import { AuditClient } from "./audit/client.js";
import { TelemetryClient } from "./telemetry/client.js";

export type Fact0Config = HttpConfig;

export class Fact0Client {
  readonly audit: AuditClient;
  readonly telemetry: TelemetryClient;
  private readonly http: HttpClient;

  constructor(config: Fact0Config) {
    this.http = createHttpClient(config);
    this.audit = new AuditClient(this.http);
    this.telemetry = new TelemetryClient(this.http);
  }
}

export { AuditClient, TelemetryClient, DEFAULT_BASE_URL };
export type { AuditEventInput, Actor, Resource } from "./audit/types.js";
