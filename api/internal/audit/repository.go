package audit

import (
	"context"
	"time"
)

// Known external identity providers used in tenant_external_identities.
// Exported as constants so HTTP transport and middleware agree on the
// canonical string instead of duplicating literals across packages.
//
// ProviderClerkOrg is retained for read-back compatibility with rows
// written before the Better Auth migration. New writes always use
// ProviderBetterAuthOrg.
const (
	ProviderBetterAuthOrg = "better_auth_org"
	ProviderClerkOrg      = "clerk_org"
)

// Repository is the storage abstraction for the audit log.
//
// It is deliberately minimal so the Postgres implementation can be replaced
// (e.g. ClickHouse) without touching the service layer. Hash-chain
// serialisation is the implementation's responsibility - callers pass an
// event whose `PrevHash` and `Hash` are already populated only by the
// service when it owns the chain transaction; for the canonical write path
// callers use AppendChained which performs the chain under a per-tenant lock.
type Repository interface {
	// AppendChained inserts a single event, computing prev_hash/hash under
	// a per-tenant serialisation barrier. The mutated event (with Hash,
	// PrevHash, SequenceNumber populated) is returned.
	AppendChained(ctx context.Context, tenantID string, e *AuditEvent) (*AuditEvent, error)

	// AppendBatchChained inserts events sequentially under a single
	// per-tenant lock, chaining each to the previous in the slice (and
	// the first to the existing tenant chain head).
	AppendBatchChained(ctx context.Context, tenantID string, events []*AuditEvent) ([]*AuditEvent, error)

	// Get fetches a single event by id, scoped to tenantID.
	Get(ctx context.Context, tenantID, id string) (*AuditEvent, error)

	// Previous returns the event immediately preceding `id` in the
	// tenant's chain. Returns nil, nil if `id` is the first event.
	Previous(ctx context.Context, tenantID, id string) (*AuditEvent, error)

	// List returns a filtered, paginated slice and the total matching
	// count for the resolved tenant.
	List(ctx context.Context, tenantID string, f Filter) ([]*AuditEvent, int, error)

	// Walk streams every event in [from,to] for the tenant in chain
	// order, invoking fn for each. Halts on the first non-nil fn error.
	Walk(ctx context.Context, tenantID string, from, to time.Time, fn func(*AuditEvent) error) (count int, err error)

	// CountByAction / CountByActorType power the PDF summary tables.
	CountByAction(ctx context.Context, tenantID string, from, to time.Time) (map[string]int, error)
	CountByActorType(ctx context.Context, tenantID string, from, to time.Time) (map[string]int, error)

	// ReanchorFrom recomputes hash and prev_hash for every event in the
	// tenant chain starting at the event with the given sequence number,
	// walking forward in sequence order. Used only by the chain re-anchor
	// service operation; called inside a serialised advisory-lock transaction.
	// Returns the number of rows updated.
	ReanchorFrom(ctx context.Context, tenantID string, fromSeq int64, startPrevHash string) (int64, error)

	// GetByID fetches a single event by id without tenant scoping - used
	// only by the re-anchor flow where the service already has the tenant.
	GetBySeq(ctx context.Context, tenantID string, seq int64) (*AuditEvent, error)
}

// TenantStore manages tenant rows and their mappings to external
// identity providers (Better Auth org, Clerk org, WorkOS org, etc.). Storing identity
// mappings separately keeps tenants.id opaque so changing auth
// providers does not require a destructive PK migration.
type TenantStore interface {
	CreateTenant(ctx context.Context, t *Tenant) error
	GetTenant(ctx context.Context, id string) (*Tenant, error)

	// GetTenantByExternalID resolves (provider, externalID) → tenant.
	// Returns a NotFound app error when no mapping exists, or when
	// the tenant has been soft-deleted.
	GetTenantByExternalID(ctx context.Context, provider, externalID string) (*Tenant, error)

	// SoftDeleteTenant marks a tenant as deleted (sets deleted_at) so
	// the audit chain is preserved for compliance even after the
	// upstream organization is deleted. Tenant lookups skip soft-deleted
	// rows. Idempotent.
	SoftDeleteTenant(ctx context.Context, id string) error

	// UpdateTenantName syncs the display name (e.g. on
	// organization.updated webhooks).
	UpdateTenantName(ctx context.Context, id, name string) error
}

// KeyStore manages API key rows.
type KeyStore interface {
	CreateKey(ctx context.Context, k *APIKey) error
	GetKeyByID(ctx context.Context, id string) (*APIKey, error)
	ListKeys(ctx context.Context, tenantID string) ([]*APIKey, error)
	RevokeKey(ctx context.Context, tenantID, id string) error

	// RevokeAllKeys revokes every active key for a tenant. Called from
	// the webhook handler when an org is deleted upstream. Idempotent.
	RevokeAllKeys(ctx context.Context, tenantID string) error
}

// WebhookStore deduplicates inbound webhook deliveries. Svix replays
// messages on transient backend failure; we must process each (source,
// message-id) exactly once.
type WebhookStore interface {
	// MarkProcessed atomically claims a webhook id. Returns (true, nil)
	// when this caller is the first to claim it; (false, nil) on
	// replay. Implementations should use INSERT ... ON CONFLICT
	// DO NOTHING to avoid races.
	MarkProcessed(ctx context.Context, id, source, eventType string) (bool, error)
}

// StatsStore exposes lightweight per-tenant counters used by the
// dashboard onboarding flow and metric tiles. Implementations should
// be cheap O(index) lookups; do not run aggregation here.
type StatsStore interface {
	// CountEvents returns the total number of audit events written by
	// this tenant. Used to detect "Setup Mode" on first dashboard load.
	CountEvents(ctx context.Context, tenantID string) (int64, error)

	// CountEventsSince returns the number of audit events written by
	// this tenant on or after `since`. Powers the "30-day rolling
	// usage" panel on the billing page.
	CountEventsSince(ctx context.Context, tenantID string, since time.Time) (int64, error)

	// CountEventsThisMonth returns audit events written in the current
	// UTC calendar month for monthly plan limit enforcement.
	CountEventsThisMonth(ctx context.Context, tenantID string) (int64, error)

	// ChainHead returns the most recent event's sequence number and
	// hash, or (0, "", nil) if the chain is empty.
	ChainHead(ctx context.Context, tenantID string) (seq int64, hash string, err error)

	// TelemetryCountsSince returns execution + span counts for a tenant since `since`.
	TelemetryCountsSince(ctx context.Context, tenantID string, since time.Time) (executions, spans int64, err error)

	// ListDistinctActions returns unique action strings since the given time.
	ListDistinctActions(ctx context.Context, tenantID string, since time.Time) ([]string, error)
}

// PlanStore exposes per-tenant plan + retention metadata.
type PlanStore interface {
	// GetTenantPlan returns the plan record bound to a tenant.
	GetTenantPlan(ctx context.Context, tenantID string) (*Plan, error)
}

// Plan is the per-customer retention / billing tier.
type Plan struct {
	ID                             string `json:"id"`
	Name                           string `json:"name"`
	RetentionDays                  *int   `json:"retention_days,omitempty"` // nil = unlimited
	MonthlyEventLimit              *int64 `json:"monthly_event_limit,omitempty"`
	MonthlyCopilotLimit            *int   `json:"monthly_copilot_limit,omitempty"` // nil = unlimited
	CopilotTranscriptRetentionDays *int   `json:"copilot_transcript_retention_days,omitempty"`
	Description                    string `json:"description,omitempty"`
}

// BootstrapInput carries the inputs to BootstrapTenant. TenantCandidate
// (id, name) is consumed only when no existing mapping is found for
// (Provider, ExternalID); KeyCandidate is consumed only when no active
// key exists for the resolved tenant. Anything not consumed is
// discarded - the caller wastes a ULID and 32B of entropy on the
// idempotent path, both of which are essentially free.
type BootstrapInput struct {
	Provider     string  // e.g. "better_auth_org"
	ExternalID   string  // e.g. organization id from Better Auth
	TenantID     string  // candidate internal id, used only when creating
	TenantName   string  // candidate display name, used only when creating
	KeyCandidate *APIKey // candidate API key, used only when no active key
}

// Bootstrapper is the atomic "ensure tenant + mapping + default key"
// primitive that powers POST /v1/me/bootstrap. The implementation must
// serialise concurrent callers for the same (Provider, ExternalID) pair
// (advisory lock, SERIALIZABLE tx, etc.) so two browser tabs racing
// the very first dashboard load can never produce two orphan tenants
// or two orphan keys.
//
// The returned `provisioned` flag is true iff the KeyCandidate was used
// (and therefore the raw key the caller holds is safe to return to the
// API client exactly once).
type Bootstrapper interface {
	BootstrapTenant(
		ctx context.Context,
		in BootstrapInput,
	) (tenant *Tenant, active *APIKey, provisioned bool, err error)
}
