package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// GetClaudeCodePolicy returns the tenant's current policy, or the version-0
// sentinel when none has been configured yet.
func (s *AuditStore) GetClaudeCodePolicy(ctx context.Context, tenantID string) (*audit.ClaudeCodePolicy, error) {
	var (
		p        audit.ClaudeCodePolicy
		rawRules []byte
	)
	err := s.readDB.QueryRow(ctx,
		`SELECT version, enforce, rules, updated_at, updated_by
		   FROM claude_code_policies WHERE tenant_id = $1`, tenantID).
		Scan(&p.Version, &p.Enforce, &rawRules, &p.UpdatedAt, &p.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &audit.ClaudeCodePolicy{Version: 0, Rules: []audit.ClaudeCodePolicyRule{}}, nil
		}
		return nil, apperrors.Internal("getting claude code policy", err)
	}
	if err := json.Unmarshal(rawRules, &p.Rules); err != nil {
		return nil, apperrors.Internal("decoding claude code policy rules", err)
	}
	if p.Rules == nil {
		p.Rules = []audit.ClaudeCodePolicyRule{}
	}
	return &p, nil
}

// PutClaudeCodePolicy performs a compare-and-swap write in a single
// statement: expectedVersion 0 creates the row at version 1; otherwise the
// update only applies when the stored version matches, bumping it by one.
// A lost CAS returns CodeConflict (HTTP 409).
func (s *AuditStore) PutClaudeCodePolicy(ctx context.Context, tenantID string, expectedVersion int,
	enforce bool, rules []audit.ClaudeCodePolicyRule, updatedBy string) (*audit.ClaudeCodePolicy, error) {

	if rules == nil {
		rules = []audit.ClaudeCodePolicyRule{}
	}
	rawRules, err := json.Marshal(rules)
	if err != nil {
		return nil, apperrors.Internal("encoding claude code policy rules", err)
	}

	var (
		version   int
		updatedAt time.Time
	)
	if expectedVersion == 0 {
		// Create: only succeeds when no row exists yet.
		err = s.writeDB.QueryRow(ctx, `
			INSERT INTO claude_code_policies (tenant_id, version, enforce, rules, updated_at, updated_by)
			VALUES ($1, 1, $2, $3::jsonb, now(), $4)
			ON CONFLICT (tenant_id) DO NOTHING
			RETURNING version, updated_at`,
			tenantID, enforce, string(rawRules), updatedBy).
			Scan(&version, &updatedAt)
	} else {
		// Update: only succeeds when the stored version matches the snapshot.
		err = s.writeDB.QueryRow(ctx, `
			UPDATE claude_code_policies
			   SET version    = version + 1,
			       enforce    = $2,
			       rules      = $3::jsonb,
			       updated_at = now(),
			       updated_by = $4
			 WHERE tenant_id = $1 AND version = $5
			RETURNING version, updated_at`,
			tenantID, enforce, string(rawRules), updatedBy, expectedVersion).
			Scan(&version, &updatedAt)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Create over an existing row, update of a stale/missing version:
			// the caller's snapshot is out of date.
			return nil, &apperrors.Error{Code: apperrors.CodeConflict, Message: "policy version conflict"}
		}
		return nil, apperrors.Internal("writing claude code policy", err)
	}

	return &audit.ClaudeCodePolicy{
		Version:   version,
		Enforce:   enforce,
		Rules:     rules,
		UpdatedAt: updatedAt,
		UpdatedBy: updatedBy,
	}, nil
}
