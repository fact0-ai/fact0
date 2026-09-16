// Package audit defines the recorded audit-log domain model.
//
// AuditEvent rows form the universal fact layer: a tamper-evident, per-tenant SHA-256 hash chain
// stored in the append-only `audit_events` table. The chain runs parallel
// to the existing execution/span/event telemetry pipeline.
package audit

import (
	"time"
)

// ─── Enumerations ──────────────────────────────────────────

// ActorType classifies who performed the audited action.
type ActorType string

const (
	ActorHuman  ActorType = "human"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

// IsValid reports whether the actor type is one of the known values.
func (t ActorType) IsValid() bool {
	switch t {
	case ActorHuman, ActorAgent, ActorSystem:
		return true
	default:
		return false
	}
}

// Outcome describes the result of an audited action.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeError   Outcome = "error"
)

// IsValid reports whether the outcome is a recognised value.
func (o Outcome) IsValid() bool {
	switch o {
	case OutcomeSuccess, OutcomeFailure, OutcomeError:
		return true
	default:
		return false
	}
}

// Scope is the permission level granted by an API key.
type Scope string

const (
	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"
)

// CanWrite reports whether the scope permits ingestion endpoints.
func (s Scope) CanWrite() bool { return s == ScopeWrite }

// IsValid reports whether the scope is recognised.
func (s Scope) IsValid() bool {
	return s == ScopeRead || s == ScopeWrite
}

// GenesisHash is the prev_hash used for the very first event in a tenant chain.
const GenesisHash = "genesis"

// ─── Domain types ──────────────────────────────────────────

// Actor describes the principal that performed the action.
type Actor struct {
	ID    string    `json:"id"`
	Type  ActorType `json:"type"`
	Email string    `json:"email,omitempty"`
}

// Resource identifies the object the action was performed on.
type Resource struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// AuditEvent is a single universal-fact-layer entry: tamper-evident audit log row.
type AuditEvent struct {
	ID             string                 `json:"id"`
	TenantID       string                 `json:"tenant_id"`
	Timestamp      time.Time              `json:"timestamp"`
	Actor          Actor                  `json:"actor"`
	Action         string                 `json:"action"`
	Resource       Resource               `json:"resource"`
	Outcome        Outcome                `json:"outcome"`
	Metadata       map[string]interface{} `json:"metadata"`
	PrevHash       string                 `json:"prev_hash"`
	Hash           string                 `json:"hash"`
	SequenceNumber int64                  `json:"sequence_number,omitempty"`
}

// Filter describes a query against the audit log. All fields are optional;
// the empty Filter returns every event for the resolved tenant.
type Filter struct {
	SessionID  string
	ActorID    string
	ActorType  ActorType
	Action     string
	ResourceID string
	Outcome    Outcome
	From       time.Time
	To         time.Time
	Page       int
	PageSize   int
}

// Normalise applies sane defaults for pagination.
func (f *Filter) Normalise() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 50
	}
	if f.PageSize > 1000 {
		f.PageSize = 1000
	}
}

// Offset returns the SQL offset corresponding to Page/PageSize.
func (f Filter) Offset() int { return (f.Page - 1) * f.PageSize }

// BrokenEvent records a single chain integrity failure within a deep scan.
type BrokenEvent struct {
	EventID        string    `json:"event_id"`
	SequenceNumber int64     `json:"sequence_number"`
	Timestamp      time.Time `json:"timestamp"`
	Action         string    `json:"action"`
	ActorID        string    `json:"actor_id"`
	Reason         string    `json:"reason"` // "hash mismatch" | "prev_hash mismatch"
}

// VerifyResult is returned from chain-integrity verification.
type VerifyResult struct {
	Valid              bool      `json:"valid"`
	TenantID           string    `json:"tenant_id"`
	EventsChecked      int       `json:"events_checked"`
	RootHash           string    `json:"root_hash"`
	From               time.Time `json:"from"`
	To                 time.Time `json:"to"`
	FirstBrokenEventID string    `json:"first_broken_event_id,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	// AllBrokenEvents is populated only when scan_all=true is requested.
	// It lists every event with a hash or prev_hash mismatch in sequence order.
	AllBrokenEvents []BrokenEvent `json:"all_broken_events,omitempty"`
	BrokenCount     int           `json:"broken_count,omitempty"`
}

// Tenant is a customer that owns an isolated audit log.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// APIKey is a credential bound to a tenant. The raw secret is never
// stored - only the SHA-256 lookup hash in `Hash`.
type APIKey struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Hash      string     `json:"-"`
	Scope     Scope      `json:"scope"`
	Label     string     `json:"label,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
