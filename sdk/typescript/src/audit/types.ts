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

export interface AuditEventInput {
  id?: string;
  timestamp?: string;
  actor: Actor;
  action: string;
  resource: Resource;
  outcome: Outcome;
  metadata?: Record<string, unknown>;
}

export interface ListEventsParams {
  actor_id?: string;
  actor_type?: ActorType;
  action?: string;
  resource_id?: string;
  outcome?: Outcome;
  from?: string;
  to?: string;
  page?: number;
  page_size?: number;
}
