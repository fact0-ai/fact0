package share

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// Link is a time-limited read-only access grant.
type Link struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	CreatedBy    string     `json:"created_by"`
	Label        string     `json:"label"`
	Scope        string     `json:"scope"`
	Token        string     `json:"token,omitempty"` // populated only on create
	FilterFrom   *time.Time `json:"filter_from,omitempty"`
	FilterTo     *time.Time `json:"filter_to,omitempty"`
	ExpiresAt    time.Time  `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	LastAccessed *time.Time `json:"last_accessed,omitempty"`
	AccessCount  int        `json:"access_count"`
	CreatedAt    time.Time  `json:"created_at"`
}

// CreateInput configures a new share link.
type CreateInput struct {
	Label      string
	FilterFrom *time.Time
	FilterTo   *time.Time
	ExpiresAt  time.Time
}

// Store persists share links.
type Store struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewStore constructs a Store.
func NewStore(pool *pgxpool.Pool, logger zerolog.Logger) *Store {
	return &Store{
		pool:   pool,
		logger: logger.With().Str("component", "share_links").Logger(),
	}
}

// Create inserts a share link row.
func (s *Store) Create(ctx context.Context, tenantID, createdBy string, in CreateInput) (*Link, error) {
	id := audit.NewShareLinkID()
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		return nil, apperrors.Internal("generating share link secret", err)
	}
	secretStr := base64.RawURLEncoding.EncodeToString(b)
	hashSum := sha256.Sum256([]byte(secretStr))
	secretHash := hex.EncodeToString(hashSum[:])

	_, err := s.pool.Exec(ctx, `
		INSERT INTO share_links (id, tenant_id, created_by, label, filter_from, filter_to, expires_at, secret_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, tenantID, createdBy, in.Label, in.FilterFrom, in.FilterTo, in.ExpiresAt, secretHash,
	)
	if err != nil {
		return nil, apperrors.Internal("creating share link", err)
	}
	link, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	link.Token = id + "_" + secretStr
	return link, nil
}

// List returns tenant share links newest first.
func (s *Store) List(ctx context.Context, tenantID string) ([]*Link, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, created_by, label, scope, filter_from, filter_to,
		       expires_at, revoked_at, last_accessed, access_count, created_at
		FROM share_links
		WHERE tenant_id = $1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, apperrors.Internal("listing share links", err)
	}
	defer rows.Close()
	var out []*Link
	for rows.Next() {
		l, err := scanLink(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Get returns a link scoped to tenant.
func (s *Store) Get(ctx context.Context, tenantID, id string) (*Link, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, created_by, label, scope, filter_from, filter_to,
		       expires_at, revoked_at, last_accessed, access_count, created_at
		FROM share_links
		WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return scanLinkRow(row.Scan)
}

// GetByID resolves a link for public access (no tenant scope on lookup).
func (s *Store) GetByID(ctx context.Context, id string) (*Link, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, created_by, label, scope, filter_from, filter_to,
		       expires_at, revoked_at, last_accessed, access_count, created_at
		FROM share_links
		WHERE id = $1`, id)
	return scanLinkRow(row.Scan)
}

// GetByIDWithSecret resolves a link for public access and returns its secret hash.
func (s *Store) GetByIDWithSecret(ctx context.Context, id string) (*Link, string, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, created_by, label, scope, filter_from, filter_to,
		       expires_at, revoked_at, last_accessed, access_count, created_at, secret_hash
		FROM share_links
		WHERE id = $1`, id)

	var l Link
	var scope string
	var secretHash *string
	err := row.Scan(
		&l.ID, &l.TenantID, &l.CreatedBy, &l.Label, &scope,
		&l.FilterFrom, &l.FilterTo, &l.ExpiresAt, &l.RevokedAt,
		&l.LastAccessed, &l.AccessCount, &l.CreatedAt, &secretHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", apperrors.NotFound("share_link", "")
		}
		return nil, "", apperrors.Internal("scanning share link", err)
	}
	l.Scope = scope
	sh := ""
	if secretHash != nil {
		sh = *secretHash
	}
	return &l, sh, nil
}

// Revoke marks a link revoked.
func (s *Store) Revoke(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE share_links SET revoked_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL`, id, tenantID)
	if err != nil {
		return apperrors.Internal("revoking share link", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("share_link", id)
	}
	return nil
}

// RecordAccess increments access count if link is valid.
func (s *Store) RecordAccess(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE share_links
		SET access_count = access_count + 1, last_accessed = NOW()
		WHERE id = $1 AND revoked_at IS NULL AND expires_at > NOW()`, id)
	if err != nil {
		return apperrors.Internal("recording share access", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("share_link", id)
	}
	return nil
}

// TenantSettings holds per-tenant configuration.
type TenantSettings struct {
	AlertWebhookURL string `json:"alert_webhook_url,omitempty"`
	AlertEmail      string `json:"alert_email,omitempty"`
	QuotaAlertMonth string `json:"quota_alert_month,omitempty"`
	QuotaAlertLevel int    `json:"quota_alert_level,omitempty"`
}

// GetSettings loads tenant settings JSON.
func (s *Store) GetSettings(ctx context.Context, tenantID string) (TenantSettings, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT settings FROM tenants WHERE id = $1`, tenantID).Scan(&raw)
	if err != nil {
		return TenantSettings{}, apperrors.Internal("loading tenant settings", err)
	}
	var out TenantSettings
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

// UpdateSettings merges settings into tenant row.
func (s *Store) UpdateSettings(ctx context.Context, tenantID string, settings TenantSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return apperrors.Internal("marshaling tenant settings", err)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tenants SET settings = $2::jsonb WHERE id = $1`, tenantID, raw)
	if err != nil {
		return apperrors.Internal("updating tenant settings: "+err.Error(), err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("tenant", tenantID)
	}
	return nil
}

func scanLink(scan func(dest ...any) error) (*Link, error) {
	var l Link
	var scope string
	err := scan(
		&l.ID, &l.TenantID, &l.CreatedBy, &l.Label, &scope,
		&l.FilterFrom, &l.FilterTo, &l.ExpiresAt, &l.RevokedAt,
		&l.LastAccessed, &l.AccessCount, &l.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.NotFound("share_link", "")
		}
		return nil, apperrors.Internal("scanning share link", err)
	}
	l.Scope = scope
	return &l, nil
}

func scanLinkRow(scan func(dest ...any) error) (*Link, error) {
	return scanLink(scan)
}

// Valid reports whether the link can be used right now.
func (l *Link) Valid(now time.Time) bool {
	if l == nil || l.RevokedAt != nil {
		return false
	}
	return now.Before(l.ExpiresAt)
}
