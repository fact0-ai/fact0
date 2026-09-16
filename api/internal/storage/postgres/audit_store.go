package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// AuditStore is the Postgres implementation of audit.Repository,
// audit.TenantStore and audit.KeyStore.
type AuditStore struct {
	ossMode bool
	writeDB *pgxpool.Pool
	readDB  *pgxpool.Pool
	logger  zerolog.Logger
}

// NewAuditStore constructs an AuditStore. readDB may equal writeDB when no
// read replica is configured.
func NewAuditStore(writeDB, readDB *pgxpool.Pool, logger zerolog.Logger) *AuditStore {
	if readDB == nil {
		readDB = writeDB
	}
	return &AuditStore{
		writeDB: writeDB,
		readDB:  readDB,
		logger:  logger.With().Str("component", "audit_store").Logger(),
	}
}

// SetOSSMode selects local entitlements without billing lookups. Set at startup.
func (s *AuditStore) SetOSSMode(enabled bool) { s.ossMode = enabled }

// WritePool exposes the write pool for components that share the primary (e.g. outbox).
func (s *AuditStore) WritePool() *pgxpool.Pool { return s.writeDB }

// ReadPool exposes the read pool for metrics and read-heavy stores.
func (s *AuditStore) ReadPool() *pgxpool.Pool { return s.readDB }

// ─── Tenants ───────────────────────────────────────────────

// CreateTenant inserts a tenant row.
func (s *AuditStore) CreateTenant(ctx context.Context, t *audit.Tenant) error {
	_, err := s.writeDB.Exec(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		t.ID, t.Name,
	)
	if err != nil {
		return apperrors.Internal("inserting tenant", err)
	}
	return nil
}

// GetTenant looks up a tenant by id. Soft-deleted tenants are hidden.
func (s *AuditStore) GetTenant(ctx context.Context, id string) (*audit.Tenant, error) {
	var t audit.Tenant
	err := s.readDB.QueryRow(ctx,
		`SELECT id, name, created_at FROM tenants WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("tenant", id)
	}
	if err != nil {
		return nil, apperrors.Internal("getting tenant", err)
	}
	return &t, nil
}

// GetTenantByExternalID resolves (provider, externalID) → tenant via
// the tenant_external_identities mapping table. Soft-deleted tenants
// are hidden, so a deleted-then-recreated Clerk org behaves as a
// fresh tenant on next bootstrap.
func (s *AuditStore) GetTenantByExternalID(ctx context.Context, provider, externalID string) (*audit.Tenant, error) {
	var t audit.Tenant
	err := s.readDB.QueryRow(ctx, `
		SELECT t.id, t.name, t.created_at
		FROM tenants t
		JOIN tenant_external_identities m ON m.tenant_id = t.id
		WHERE m.provider = $1 AND m.external_id = $2 AND t.deleted_at IS NULL`,
		provider, externalID,
	).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("tenant", provider+":"+externalID)
	}
	if err != nil {
		return nil, apperrors.Internal("getting tenant by external id", err)
	}
	return &t, nil
}

// SoftDeleteTenant marks a tenant as deleted without removing rows -
// the audit chain is preserved for compliance even after the upstream
// Clerk org disappears. Idempotent: re-deleting a soft-deleted tenant
// is a no-op.
func (s *AuditStore) SoftDeleteTenant(ctx context.Context, id string) error {
	_, err := s.writeDB.Exec(ctx,
		`UPDATE tenants SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return apperrors.Internal("soft-deleting tenant", err)
	}
	return nil
}

// UpdateTenantName syncs the tenant display name. No-op if not found.
func (s *AuditStore) UpdateTenantName(ctx context.Context, id, name string) error {
	_, err := s.writeDB.Exec(ctx,
		`UPDATE tenants SET name = $2 WHERE id = $1 AND deleted_at IS NULL`,
		id, name,
	)
	if err != nil {
		return apperrors.Internal("updating tenant name", err)
	}
	return nil
}

// BootstrapTenant atomically ensures (a) a tenant row exists for the
// given (provider, externalID) mapping and (b) that tenant has at
// least one active API key. The whole upsert runs in a single tx
// guarded by an advisory lock keyed on (provider, externalID), so
// concurrent racing browser tabs serialise here and we never produce
// two orphan tenants or two orphan keys for the same fresh org.
//
// `in.TenantID` / `in.TenantName` are consumed only when no mapping
// exists; `in.KeyCandidate` is consumed only when no active key
// exists. Discarded inputs are not an error.
func (s *AuditStore) BootstrapTenant(
	ctx context.Context,
	in audit.BootstrapInput,
) (*audit.Tenant, *audit.APIKey, bool, error) {
	if in.Provider == "" || in.ExternalID == "" {
		return nil, nil, false, apperrors.InvalidInput("provider and external_id are required")
	}
	if in.KeyCandidate == nil {
		return nil, nil, false, apperrors.InvalidInput("key candidate is required")
	}

	tx, err := s.writeDB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, false, apperrors.Internal("beginning bootstrap tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialise on the external identity, not the internal tenant id -
	// on the first-call path the internal id doesn't exist yet, and
	// racing callers must agree on it before either of them creates
	// the tenant row.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1))`,
		in.Provider+"|"+in.ExternalID,
	); err != nil {
		return nil, nil, false, apperrors.Internal("acquiring bootstrap lock", err)
	}

	t, err := lookupTenantByExternalTx(ctx, tx, in.Provider, in.ExternalID)
	if err != nil {
		return nil, nil, false, err
	}
	if t == nil {
		// First time we've seen this external identity - provision both
		// the tenant row and the mapping atomically.
		if in.TenantID == "" {
			return nil, nil, false, apperrors.InvalidInput("tenant id candidate is required for new tenants")
		}
		var created audit.Tenant
		if err := tx.QueryRow(ctx, `
			INSERT INTO tenants (id, name) VALUES ($1, $2)
			RETURNING id, name, created_at`,
			in.TenantID, in.TenantName,
		).Scan(&created.ID, &created.Name, &created.CreatedAt); err != nil {
			return nil, nil, false, apperrors.Internal("inserting tenant", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO tenant_external_identities (provider, external_id, tenant_id)
			VALUES ($1, $2, $3)`,
			in.Provider, in.ExternalID, created.ID,
		); err != nil {
			return nil, nil, false, apperrors.Internal("inserting external identity mapping", err)
		}
		t = &created
	}

	active, err := firstActiveKeyTx(ctx, tx, t.ID)
	if err != nil {
		return nil, nil, false, err
	}
	if active != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, false, apperrors.Internal("committing bootstrap tx", err)
		}
		return t, active, false, nil
	}

	cand := in.KeyCandidate
	cand.TenantID = t.ID
	if err := tx.QueryRow(ctx, `
		INSERT INTO api_keys (id, tenant_id, key_hash, scope, label)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at`,
		cand.ID, cand.TenantID, cand.Hash,
		string(cand.Scope), nullableString(cand.Label),
	).Scan(&cand.CreatedAt); err != nil {
		return nil, nil, false, apperrors.Internal("inserting bootstrap api key", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, false, apperrors.Internal("committing bootstrap tx", err)
	}
	return t, cand, true, nil
}

func lookupTenantByExternalTx(ctx context.Context, tx pgx.Tx, provider, externalID string) (*audit.Tenant, error) {
	var t audit.Tenant
	err := tx.QueryRow(ctx, `
		SELECT t.id, t.name, t.created_at
		FROM tenants t
		JOIN tenant_external_identities m ON m.tenant_id = t.id
		WHERE m.provider = $1 AND m.external_id = $2`,
		provider, externalID,
	).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.Internal("looking up tenant by external id in bootstrap", err)
	}
	return &t, nil
}

func firstActiveKeyTx(ctx context.Context, tx pgx.Tx, tenantID string) (*audit.APIKey, error) {
	var (
		k     audit.APIKey
		label *string
		scope string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, tenant_id, key_hash, scope, label, created_at, revoked_at
		FROM api_keys WHERE tenant_id = $1 AND revoked_at IS NULL
		ORDER BY created_at ASC LIMIT 1`, tenantID,
	).Scan(&k.ID, &k.TenantID, &k.Hash, &scope, &label, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.Internal("looking up active key in bootstrap", err)
	}
	k.Scope = audit.Scope(scope)
	if label != nil {
		k.Label = *label
	}
	return &k, nil
}

// ─── API keys ──────────────────────────────────────────────

// CreateKey inserts a new API key row. The raw secret must already have
// been hashed by the caller - only `Hash` is persisted.
func (s *AuditStore) CreateKey(ctx context.Context, k *audit.APIKey) error {
	_, err := s.writeDB.Exec(ctx, `
		INSERT INTO api_keys (id, tenant_id, key_hash, scope, label)
		VALUES ($1, $2, $3, $4, $5)`,
		k.ID, k.TenantID, k.Hash, string(k.Scope), nullableString(k.Label),
	)
	if err != nil {
		return apperrors.Internal("inserting api key", err)
	}
	return nil
}

// GetKeyByID returns an active key matching the provided ID.
// Revoked keys are returned with RevokedAt set so callers can return 401.
func (s *AuditStore) GetKeyByID(ctx context.Context, id string) (*audit.APIKey, error) {
	var k audit.APIKey
	var label *string
	var scope string
	err := s.readDB.QueryRow(ctx, `
		SELECT id, tenant_id, key_hash, scope, label, created_at, revoked_at
		FROM api_keys WHERE id = $1`, id,
	).Scan(&k.ID, &k.TenantID, &k.Hash, &scope, &label, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("api_key", "")
	}
	if err != nil {
		return nil, apperrors.Internal("looking up api key", err)
	}
	k.Scope = audit.Scope(scope)
	if label != nil {
		k.Label = *label
	}
	return &k, nil
}

// ListKeys returns every key (active and revoked) bound to a tenant,
// most-recently-created first. Hashes are loaded but should never be
// returned to API clients.
func (s *AuditStore) ListKeys(ctx context.Context, tenantID string) ([]*audit.APIKey, error) {
	rows, err := s.readDB.Query(ctx, `
		SELECT id, tenant_id, key_hash, scope, label, created_at, revoked_at
		FROM api_keys WHERE tenant_id = $1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, apperrors.Internal("listing api keys", err)
	}
	defer rows.Close()

	var out []*audit.APIKey
	for rows.Next() {
		var k audit.APIKey
		var label *string
		var scope string
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Hash, &scope, &label, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, apperrors.Internal("scanning api key", err)
		}
		k.Scope = audit.Scope(scope)
		if label != nil {
			k.Label = *label
		}
		out = append(out, &k)
	}
	return out, nil
}

// CountEvents returns the total number of audit events stored for the
// tenant. Cheap - the per-tenant index makes this O(1) in practice.
func (s *AuditStore) CountEvents(ctx context.Context, tenantID string) (int64, error) {
	var n int64
	err := s.readDB.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_events WHERE tenant_id = $1`, tenantID,
	).Scan(&n)
	if err != nil {
		return 0, apperrors.Internal("counting events", err)
	}
	return n, nil
}

// CountEventsSince returns the number of audit events for a tenant on
// or after `since`. Uses the (tenant_id, timestamp DESC) index for an
// index-only scan.
func (s *AuditStore) CountEventsSince(ctx context.Context, tenantID string, since time.Time) (int64, error) {
	var n int64
	err := s.readDB.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE tenant_id = $1 AND timestamp >= $2`, tenantID, since,
	).Scan(&n)
	if err != nil {
		return 0, apperrors.Internal("counting recent events", err)
	}
	return n, nil
}

// CountEventsThisMonth counts audit events in the current UTC calendar month.
func (s *AuditStore) CountEventsThisMonth(ctx context.Context, tenantID string) (int64, error) {
	var n int64
	err := s.readDB.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE tenant_id = $1
		  AND timestamp >= date_trunc('month', NOW() AT TIME ZONE 'UTC')`, tenantID,
	).Scan(&n)
	if err != nil {
		return 0, apperrors.Internal("counting monthly events", err)
	}
	return n, nil
}

// TelemetryCountsSince returns tenant-scoped execution + span counts since `since`.
func (s *AuditStore) TelemetryCountsSince(ctx context.Context, tenantID string, since time.Time) (int64, int64, error) {
	var execs, spans int64
	if err := s.readDB.QueryRow(ctx,
		`SELECT COUNT(*) FROM executions WHERE tenant_id = $1 AND started_at >= $2`, tenantID, since,
	).Scan(&execs); err != nil {
		return 0, 0, apperrors.Internal("counting executions", err)
	}
	if err := s.readDB.QueryRow(ctx, `
		SELECT COUNT(*) FROM spans s
		JOIN executions e ON e.id = s.execution_id
		WHERE e.tenant_id = $1 AND s.started_at >= $2`, tenantID, since,
	).Scan(&spans); err != nil {
		return 0, 0, apperrors.Internal("counting spans", err)
	}
	return execs, spans, nil
}

// ListDistinctActions returns unique action values for a tenant since `since`.
func (s *AuditStore) ListDistinctActions(ctx context.Context, tenantID string, since time.Time) ([]string, error) {
	rows, err := s.readDB.Query(ctx, `
		SELECT DISTINCT action FROM audit_events
		WHERE tenant_id = $1 AND timestamp >= $2
		ORDER BY action ASC`, tenantID, since)
	if err != nil {
		return nil, apperrors.Internal("listing distinct actions", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, apperrors.Internal("scanning action", err)
		}
		out = append(out, action)
	}
	return out, rows.Err()
}

// GetTenantPlan returns the plan record currently bound to a tenant.
// Returns NotFound for an unknown tenant; falls back to the `free` plan
// if the tenant exists but has no plan_id (shouldn't happen after
// migration 007 which sets a default).
func (s *AuditStore) GetTenantPlan(ctx context.Context, tenantID string) (*audit.Plan, error) {
	if s.ossMode {
		if _, err := s.GetTenant(ctx, tenantID); err != nil {
			return nil, err
		}
		return &audit.Plan{ID: "oss", Name: "Self-hosted", Description: "All core features; operator-managed storage and retention."}, nil
	}
	var p audit.Plan
	var rdays *int
	var copilotDays *int
	var monthlyLimit *int64
	var copilotLimit *int
	var periodEnd *time.Time
	var subStatus *string
	err := s.readDB.QueryRow(ctx, `
		SELECT p.id, p.name, p.retention_days, p.copilot_transcript_retention_days,
		       p.description, p.monthly_event_limit, p.monthly_copilot_limit,
		       t.subscription_current_period_end, t.subscription_status
		FROM tenants t
		JOIN plans p ON p.id = t.plan_id
		WHERE t.id = $1 AND t.deleted_at IS NULL`, tenantID,
	).Scan(&p.ID, &p.Name, &rdays, &copilotDays, &p.Description, &monthlyLimit, &copilotLimit, &periodEnd, &subStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("tenant", tenantID)
	}
	if err != nil {
		return nil, apperrors.Internal("getting tenant plan", err)
	}

	// Dynamic self-healing downgrade if Pro plan promotional period has expired
	if periodEnd != nil && periodEnd.Before(time.Now().UTC()) && p.ID == "pro" {
		_, _ = s.writeDB.Exec(ctx, `
			UPDATE tenants
			SET plan_id = 'free',
			    subscription_status = 'expired'
			WHERE id = $1 AND deleted_at IS NULL`, tenantID)

		// Fetch free plan details to return instead
		err = s.readDB.QueryRow(ctx, `
			SELECT id, name, retention_days, copilot_transcript_retention_days,
			       description, monthly_event_limit, monthly_copilot_limit
			FROM plans
			WHERE id = 'free'`).Scan(&p.ID, &p.Name, &rdays, &copilotDays, &p.Description, &monthlyLimit, &copilotLimit)
		if err != nil {
			return nil, apperrors.Internal("getting tenant plan fallback", err)
		}
	}

	p.RetentionDays = rdays
	p.CopilotTranscriptRetentionDays = copilotDays
	p.MonthlyEventLimit = monthlyLimit
	p.MonthlyCopilotLimit = copilotLimit
	return &p, nil
}

// ChainHead returns the latest (sequence, hash) for the tenant's chain.
// Returns (0, "", nil) when the chain is empty.
func (s *AuditStore) ChainHead(ctx context.Context, tenantID string) (int64, string, error) {
	var seq int64
	var hash string
	err := s.readDB.QueryRow(ctx, `
		SELECT sequence_number, hash FROM audit_events
		WHERE tenant_id = $1
		ORDER BY sequence_number DESC LIMIT 1`, tenantID,
	).Scan(&seq, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", apperrors.Internal("reading chain head", err)
	}
	return seq, hash, nil
}

// RevokeKey marks a key as revoked. No-op if already revoked.
func (s *AuditStore) RevokeKey(ctx context.Context, tenantID, id string) error {
	tag, err := s.writeDB.Exec(ctx, `
		UPDATE api_keys SET revoked_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL`, id, tenantID)
	if err != nil {
		return apperrors.Internal("revoking api key", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("api_key", id)
	}
	return nil
}

// RevokeAllKeys revokes every active key for a tenant. Used when an
// org is deleted upstream - the audit chain stays readable for
// compliance but no new events can be appended.
func (s *AuditStore) RevokeAllKeys(ctx context.Context, tenantID string) error {
	_, err := s.writeDB.Exec(ctx, `
		UPDATE api_keys SET revoked_at = NOW()
		WHERE tenant_id = $1 AND revoked_at IS NULL`, tenantID)
	if err != nil {
		return apperrors.Internal("revoking all api keys", err)
	}
	return nil
}

// MarkProcessed atomically claims a webhook id. Returns (true, nil)
// if this caller was the first to insert it; (false, nil) on replay.
// Race-free: Postgres serialises the ON CONFLICT DO NOTHING.
func (s *AuditStore) MarkProcessed(ctx context.Context, id, source, eventType string) (bool, error) {
	tag, err := s.writeDB.Exec(ctx, `
		INSERT INTO processed_webhooks (id, source, event_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING`,
		id, source, eventType,
	)
	if err != nil {
		return false, apperrors.Internal("marking webhook processed", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ─── Audit events ──────────────────────────────────────────

// AppendChained inserts a single event under a per-tenant advisory lock.
// The lock is held for the lifetime of the transaction so concurrent
// writers for the same tenant serialise on the chain head.
func (s *AuditStore) AppendChained(ctx context.Context, tenantID string, e *audit.AuditEvent) (*audit.AuditEvent, error) {
	out, err := s.AppendBatchChained(ctx, tenantID, []*audit.AuditEvent{e})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, apperrors.Internal("append returned no events", nil)
	}
	return out[0], nil
}

// bulkInsertChunkSize is the max rows per INSERT statement in AppendBatchChained.
const bulkInsertChunkSize = 100

// GetAdvisoryLockKey maps a tenant ID to a deterministic 64-bit bigint key using SHA-256,
// eliminating the collision risks of postgres hashtext().
func GetAdvisoryLockKey(tenantID string) int64 {
	h := sha256.Sum256([]byte(tenantID))
	return int64(binary.BigEndian.Uint64(h[:8]))
}

// AppendBatchChained inserts up to N events within a single transaction using
// chunked multi-row INSERTs. Duplicate event IDs (idempotent retries) are skipped.
func (s *AuditStore) AppendBatchChained(ctx context.Context, tenantID string, events []*audit.AuditEvent) ([]*audit.AuditEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	start := time.Now()
	defer func() {
		metrics.AuditChainCommitDuration.Observe(time.Since(start).Seconds())
		metrics.AuditBatchEvents.Observe(float64(len(events)))
	}()

	tx, err := s.writeDB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, apperrors.Internal("beginning audit tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, GetAdvisoryLockKey(tenantID)); err != nil {
		return nil, apperrors.Internal("acquiring tenant lock", err)
	}

	prevHash, err := lastHashTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	existing, err := loadExistingEventsTx(ctx, tx, tenantID, ids)
	if err != nil {
		return nil, err
	}

	toInsert := make([]*audit.AuditEvent, 0, len(events))
	for _, e := range events {
		e.TenantID = tenantID
		if _, ok := existing[e.ID]; ok {
			continue
		}
		e.Timestamp = e.Timestamp.UTC().Truncate(time.Microsecond)
		if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('audit_events','sequence_number'))`).Scan(&e.SequenceNumber); err != nil {
			return nil, apperrors.Internal("allocating audit sequence", err)
		}
		e.PrevHash = prevHash
		e.Hash = audit.ComputeHash(e)
		if e.Hash == "" {
			return nil, apperrors.InvalidInput("audit metadata is not canonical JSON")
		}
		toInsert = append(toInsert, e)
		existing[e.ID] = e // deduplicate repeated IDs within this batch too
		prevHash = e.Hash
	}

	if len(toInsert) > 0 {
		if err := bulkInsertAuditEventsTx(ctx, tx, toInsert); err != nil {
			return nil, err
		}
	}

	out := make([]*audit.AuditEvent, 0, len(events))
	for _, e := range events {
		if ex, ok := existing[e.ID]; ok {
			out = append(out, ex)
			continue
		}
		out = append(out, e)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, apperrors.Internal("committing audit tx", err)
	}
	return out, nil
}

func loadExistingEventsTx(ctx context.Context, tx pgx.Tx, tenantID string, ids []string) (map[string]*audit.AuditEvent, error) {
	out := make(map[string]*audit.AuditEvent)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, selectAuditCols+` WHERE tenant_id = $1 AND id = ANY($2)`, tenantID, ids)
	if err != nil {
		return nil, apperrors.Internal("checking existing audit events", err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanAuditRow(rows)
		if err != nil {
			return nil, apperrors.Internal("scanning existing audit event", err)
		}
		out[e.ID] = e
	}
	return out, rows.Err()
}

func bulkInsertAuditEventsTx(ctx context.Context, tx pgx.Tx, events []*audit.AuditEvent) error {
	for start := 0; start < len(events); start += bulkInsertChunkSize {
		end := start + bulkInsertChunkSize
		if end > len(events) {
			end = len(events)
		}
		if err := bulkInsertChunkTx(ctx, tx, events[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func bulkInsertChunkTx(ctx context.Context, tx pgx.Tx, events []*audit.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}

	colsPerRow := 15
	args := make([]interface{}, 0, len(events)*colsPerRow)
	var sb strings.Builder
	sb.WriteString(`INSERT INTO audit_events (id, tenant_id, timestamp, actor_id, actor_type, actor_email, action, resource_id, resource_type, resource_name, outcome, metadata, prev_hash, hash, sequence_number) VALUES `)

	argN := 1
	for i, e := range events {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('(')
		for c := 0; c < colsPerRow; c++ {
			if c > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(argN))
			argN++
		}
		sb.WriteByte(')')

		md, err := json.Marshal(e.Metadata)
		if err != nil {
			return apperrors.Internal("marshaling metadata", err)
		}
		args = append(args,
			e.ID, e.TenantID, e.Timestamp.UTC(),
			e.Actor.ID, string(e.Actor.Type), e.Actor.Email,
			e.Action, e.Resource.ID, e.Resource.Type, e.Resource.Name,
			string(e.Outcome), json.RawMessage(md), e.PrevHash, e.Hash, e.SequenceNumber,
		)
	}
	sb.WriteString(` RETURNING id, sequence_number`)

	rows, err := tx.Query(ctx, sb.String(), args...)
	if err != nil {
		return apperrors.Internal("bulk inserting audit events", err)
	}
	defer rows.Close()

	seqByID := make(map[string]int64, len(events))
	for rows.Next() {
		var id string
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			return apperrors.Internal("scanning bulk insert result", err)
		}
		seqByID[id] = seq
	}
	if err := rows.Err(); err != nil {
		return apperrors.Internal("bulk insert rows", err)
	}
	for _, e := range events {
		seq, ok := seqByID[e.ID]
		if !ok {
			return apperrors.Internal(fmt.Sprintf("missing sequence for inserted event %s", e.ID), nil)
		}
		e.SequenceNumber = seq
	}
	return nil
}

func lastHashTx(ctx context.Context, tx pgx.Tx, tenantID string) (string, error) {
	var h string
	err := tx.QueryRow(ctx, `
		SELECT hash FROM audit_events
		WHERE tenant_id = $1
		ORDER BY sequence_number DESC
		LIMIT 1`, tenantID).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return audit.GenesisHash, nil
	}
	if err != nil {
		return "", apperrors.Internal("reading chain head", err)
	}
	return h, nil
}

// Get returns a single event scoped to the resolved tenant.
func (s *AuditStore) Get(ctx context.Context, tenantID, id string) (*audit.AuditEvent, error) {
	row := s.readDB.QueryRow(ctx, selectAuditCols+` WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	e, err := scanAuditRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("audit_event", id)
	}
	if err != nil {
		return nil, apperrors.Internal("getting audit event", err)
	}
	return e, nil
}

// Previous returns the event preceding `id` in the chain, or nil if `id` is
// the first event for the tenant.
func (s *AuditStore) Previous(ctx context.Context, tenantID, id string) (*audit.AuditEvent, error) {
	var seq int64
	err := s.readDB.QueryRow(ctx,
		`SELECT sequence_number FROM audit_events WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("audit_event", id)
	}
	if err != nil {
		return nil, apperrors.Internal("looking up sequence", err)
	}

	row := s.readDB.QueryRow(ctx, selectAuditCols+`
		WHERE tenant_id = $1 AND sequence_number < $2
		ORDER BY sequence_number DESC LIMIT 1`, tenantID, seq)
	e, err := scanAuditRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.Internal("getting previous event", err)
	}
	return e, nil
}

// List returns a filtered, paginated slice and the total matching count.
func (s *AuditStore) List(ctx context.Context, tenantID string, f audit.Filter) ([]*audit.AuditEvent, int, error) {
	f.Normalise()

	whereSQL, args := buildAuditFilter(tenantID, f)

	var total int
	if err := s.readDB.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, apperrors.Internal("counting audit events", err)
	}

	args = append(args, f.PageSize, f.Offset())
	q := selectAuditCols + ` ` + whereSQL +
		fmt.Sprintf(" ORDER BY sequence_number DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := s.readDB.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, apperrors.Internal("listing audit events", err)
	}
	defer rows.Close()

	var out []*audit.AuditEvent
	for rows.Next() {
		e, err := scanAuditRow(rows)
		if err != nil {
			return nil, 0, apperrors.Internal("scanning audit event", err)
		}
		out = append(out, e)
	}
	return out, total, nil
}

// Walk iterates every event in [from,to] in chain order.
func (s *AuditStore) Walk(ctx context.Context, tenantID string, from, to time.Time, fn func(*audit.AuditEvent) error) (int, error) {
	var lastSeq int64 = -1
	count := 0
	pageSize := 1000

	for {
		q := selectAuditCols + `
			WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp <= $3 AND sequence_number > $4
			ORDER BY sequence_number ASC
			LIMIT $5`
		rows, err := s.readDB.Query(ctx, q, tenantID, from.UTC(), to.UTC(), lastSeq, pageSize)
		if err != nil {
			return count, apperrors.Internal("walking audit events chunk", err)
		}

		chunkSize := 0
		err = func() error {
			defer rows.Close()
			for rows.Next() {
				e, err := scanAuditRow(rows)
				if err != nil {
					return apperrors.Internal("scanning audit event chunk", err)
				}
				if err := fn(e); err != nil {
					return err
				}
				lastSeq = e.SequenceNumber
				chunkSize++
				count++
			}
			return rows.Err()
		}()
		if err != nil {
			return count, err
		}

		if chunkSize < pageSize {
			break
		}
	}

	return count, nil
}

// CountByAction groups events by `action` within a window.
func (s *AuditStore) CountByAction(ctx context.Context, tenantID string, from, to time.Time) (map[string]int, error) {
	return s.groupCount(ctx, "action", tenantID, from, to)
}

// CountByActorType groups events by `actor_type` within a window.
func (s *AuditStore) CountByActorType(ctx context.Context, tenantID string, from, to time.Time) (map[string]int, error) {
	return s.groupCount(ctx, "actor_type", tenantID, from, to)
}

func (s *AuditStore) groupCount(ctx context.Context, col, tenantID string, from, to time.Time) (map[string]int, error) {
	q := fmt.Sprintf(`
		SELECT %s, COUNT(*) FROM audit_events
		WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp <= $3
		GROUP BY %s ORDER BY 2 DESC`, col, col)
	rows, err := s.readDB.Query(ctx, q, tenantID, from.UTC(), to.UTC())
	if err != nil {
		return nil, apperrors.Internal("group count", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, apperrors.Internal("scanning group count", err)
		}
		out[k] = n
	}
	return out, nil
}

// GetBySeq returns a single event identified by tenant + sequence number.
func (s *AuditStore) GetBySeq(ctx context.Context, tenantID string, seq int64) (*audit.AuditEvent, error) {
	row := s.readDB.QueryRow(ctx, selectAuditCols+` WHERE tenant_id = $1 AND sequence_number = $2`, tenantID, seq)
	e, err := scanAuditRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("audit_event", fmt.Sprintf("seq=%d", seq))
	}
	if err != nil {
		return nil, apperrors.Internal("getting audit event by seq", err)
	}
	return e, nil
}

// ReanchorFrom walks every event from fromSeq onward (inclusive) for the
// given tenant in sequence order, recomputes hash/prev_hash, and writes
// them back. Runs inside a caller-supplied transaction for atomicity.
// The trigger audit_events_reject_chain_changes only blocks changes to
// hash-critical *data* columns (actor_id, action, etc.) but explicitly
// allows hash/prev_hash updates to support re-anchoring.
func (s *AuditStore) ReanchorFrom(ctx context.Context, tenantID string, fromSeq int64, startPrevHash string) (int64, error) {
	tx, err := s.writeDB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, apperrors.Internal("beginning reanchor tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialise against concurrent ingest for this tenant.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, tenantID); err != nil {
		return 0, apperrors.Internal("acquiring tenant lock for reanchor", err)
	}

	// Allow the trigger to pass hash/prev_hash updates in this transaction.
	if _, err := tx.Exec(ctx, `SET LOCAL "fact0.reanchor_mode" = 'on'`); err != nil {
		return 0, apperrors.Internal("setting reanchor mode", err)
	}

	rows, err := tx.Query(ctx, selectAuditCols+`
		WHERE tenant_id = $1 AND sequence_number >= $2
		ORDER BY sequence_number ASC`, tenantID, fromSeq)
	if err != nil {
		return 0, apperrors.Internal("querying events for reanchor", err)
	}

	type patch struct {
		id       string
		seq      int64
		prevHash string
		hash     string
	}

	var patches []patch
	prevHash := startPrevHash
	for rows.Next() {
		e, sErr := scanAuditRow(rows)
		if sErr != nil {
			rows.Close()
			return 0, apperrors.Internal("scanning event for reanchor", sErr)
		}
		e.PrevHash = prevHash
		e.Hash = audit.ComputeHash(e)
		patches = append(patches, patch{id: e.ID, seq: e.SequenceNumber, prevHash: e.PrevHash, hash: e.Hash})
		prevHash = e.Hash
	}
	rows.Close()

	var updated int64
	for _, p := range patches {
		tag, uErr := tx.Exec(ctx,
			`UPDATE audit_events SET prev_hash = $1, hash = $2 WHERE tenant_id = $3 AND sequence_number = $4`,
			p.prevHash, p.hash, tenantID, p.seq)
		if uErr != nil {
			return 0, apperrors.Internal(fmt.Sprintf("updating hash for seq %d", p.seq), uErr)
		}
		updated += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, apperrors.Internal("committing reanchor tx", err)
	}
	return updated, nil
}

// ─── Helpers ───────────────────────────────────────────────

const selectAuditCols = `
	SELECT id, tenant_id, timestamp, actor_id, actor_type, actor_email,
	       action, resource_id, resource_type, resource_name,
	       outcome, metadata, prev_hash, hash, sequence_number
	FROM audit_events`

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAuditRow(r rowScanner) (*audit.AuditEvent, error) {
	var (
		e         audit.AuditEvent
		actorType string
		outcome   string
		md        []byte
	)
	if err := r.Scan(
		&e.ID, &e.TenantID, &e.Timestamp,
		&e.Actor.ID, &actorType, &e.Actor.Email,
		&e.Action, &e.Resource.ID, &e.Resource.Type, &e.Resource.Name,
		&outcome, &md, &e.PrevHash, &e.Hash, &e.SequenceNumber,
	); err != nil {
		return nil, err
	}
	e.Actor.Type = audit.ActorType(actorType)
	e.Outcome = audit.Outcome(outcome)
	if len(md) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(md))
		decoder.UseNumber()
		if err := decoder.Decode(&e.Metadata); err != nil {
			return nil, err
		}
	}
	return &e, nil
}

func buildAuditFilter(tenantID string, f audit.Filter) (string, []interface{}) {
	args := []interface{}{tenantID}
	clauses := []string{"tenant_id = $1"}
	add := func(clause string, val interface{}) {
		args = append(args, val)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}

	if f.SessionID != "" {
		args = append(args, f.SessionID)
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(metadata->>'session_id' = $%d OR (resource_type = 'claude_code.session' AND resource_id = $%d))", n, n))
	}
	if f.ActorID != "" {
		add("actor_id = $%d", f.ActorID)
	}
	if f.ActorType != "" {
		add("actor_type = $%d", string(f.ActorType))
	}
	if f.Action != "" {
		if strings.HasSuffix(f.Action, "*") {
			prefix := strings.TrimSuffix(f.Action, "*")
			prefix = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
			add("action LIKE $%d", prefix+"%")
		} else {
			add("action = $%d", f.Action)
		}
	}
	if f.ResourceID != "" {
		add("resource_id = $%d", f.ResourceID)
	}
	if f.Outcome != "" {
		add("outcome = $%d", string(f.Outcome))
	}
	if !f.From.IsZero() {
		add("timestamp >= $%d", f.From.UTC())
	}
	if !f.To.IsZero() {
		add("timestamp <= $%d", f.To.UTC())
	}

	where := "WHERE " + clauses[0]
	for _, c := range clauses[1:] {
		where += " AND " + c
	}
	return where, args
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
