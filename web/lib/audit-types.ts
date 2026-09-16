// Wire types for the Audit Log API. Mirror internal/audit/model.go.

export type ActorType = "human" | "agent" | "system";
export type Outcome = "success" | "failure" | "error";

export interface Actor {
  id: string;
  type: ActorType;
  email?: string;
}

export interface Resource {
  id: string;
  type: string;
  name?: string;
}

export interface AuditEvent {
  id: string;
  tenant_id: string;
  timestamp: string;
  actor: Actor;
  action: string;
  resource: Resource;
  outcome: Outcome;
  metadata?: Record<string, unknown>;
  prev_hash: string;
  hash: string;
  sequence_number?: number;
}

export interface AuditFilter {
  session_id?: string;
  actor_id?: string;
  actor_type?: ActorType | "";
  action?: string;
  resource_id?: string;
  outcome?: Outcome | "";
  from?: string;
  to?: string;
  page?: number;
  page_size?: number;
}

export interface AuditListResponse {
  events: AuditEvent[];
  total: number;
  page: number;
  page_size: number;
}

export interface BrokenEvent {
  event_id: string;
  sequence_number: number;
  timestamp: string;
  action: string;
  actor_id: string;
  reason: "hash mismatch" | "prev_hash mismatch";
}

export interface VerifyResult {
  valid: boolean;
  tenant_id: string;
  events_checked: number;
  root_hash: string;
  from: string;
  to: string;
  first_broken_event_id?: string;
  reason?: string;
  /** Populated only when scan_all=true - every event with a chain failure. */
  all_broken_events?: BrokenEvent[];
  broken_count?: number;
}

export interface ReanchorResult {
  status: "reanchored";
  meta_event_id: string;
  meta_event_seq: number;
}
