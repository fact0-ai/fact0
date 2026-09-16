package postgres

import (
	"context"
	"errors"

	"github.com/fact0-ai/fact0/internal/audit"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
	"github.com/jackc/pgx/v5"
)

// TenantIDForOrg returns the internal tenant id mapped to a Better Auth org.
func (s *AdminStore) TenantIDForOrg(ctx context.Context, orgID string) (string, bool) {
	if orgID == "" {
		return "", false
	}
	var tenantID string
	err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM tenant_external_identities
		WHERE provider = $1 AND external_id = $2
	`, audit.ProviderBetterAuthOrg, orgID).Scan(&tenantID)
	if err != nil {
		return "", false
	}
	return tenantID, true
}

// Owned workspaces (orgs) and their tenants are fully purged, including
// audit chains - intended for founder-only testing (e.g. re-signup email flows).
func (s *AdminStore) HardDeleteUser(ctx context.Context, actorUserID, targetUserID string) error {
	if actorUserID == targetUserID {
		return apperrors.InvalidInput("cannot delete your own account")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperrors.Internal("begin hard delete tx", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var email string
	err = tx.QueryRow(ctx, `SELECT email FROM "user" WHERE id = $1`, targetUserID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperrors.NotFound("user", targetUserID)
	}
	if err != nil {
		return apperrors.Internal("load user for delete", err)
	}

	orgRows, err := tx.Query(ctx, `
		SELECT m."organizationId"
		FROM "member" m
		WHERE m."userId" = $1 AND m.role = 'owner'
	`, targetUserID)
	if err != nil {
		return apperrors.Internal("list owned orgs", err)
	}
	defer orgRows.Close()

	var orgIDs []string
	for orgRows.Next() {
		var orgID string
		if err := orgRows.Scan(&orgID); err != nil {
			return apperrors.Internal("scan owned org", err)
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := orgRows.Err(); err != nil {
		return apperrors.Internal("iterate owned orgs", err)
	}

	tenantSeen := make(map[string]struct{})
	for _, orgID := range orgIDs {
		var tenantID *string
		err := tx.QueryRow(ctx, `
			SELECT tenant_id FROM tenant_external_identities
			WHERE provider = $1 AND external_id = $2
		`, audit.ProviderBetterAuthOrg, orgID).Scan(&tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return apperrors.Internal("resolve tenant for org", err)
		}
		if tenantID != nil && *tenantID != "" {
			if _, ok := tenantSeen[*tenantID]; !ok {
				tenantSeen[*tenantID] = struct{}{}
				if err := purgeTenantHard(ctx, tx, *tenantID); err != nil {
					return err
				}
			}
		}
	}

	if len(orgIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM "organization" WHERE id = ANY($1)`, orgIDs); err != nil {
			return apperrors.Internal("delete owned organizations", err)
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM copilot_sessions WHERE user_id = $1`, targetUserID); err != nil {
		return apperrors.Internal("delete copilot sessions", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM copilot_usage WHERE user_id = $1`, targetUserID); err != nil {
		return apperrors.Internal("delete copilot usage", err)
	}

	if err := deleteAuditEventsWhere(ctx, tx, `actor_id = $1`, targetUserID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM "invitation" WHERE LOWER(email) = LOWER($1)`, email); err != nil {
		return apperrors.Internal("delete invitations", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM "verification" WHERE LOWER(identifier) = LOWER($1)`, email); err != nil {
		return apperrors.Internal("delete verification tokens", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, targetUserID)
	if err != nil {
		return apperrors.Internal("delete user", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("user", targetUserID)
	}

	if err := tx.Commit(ctx); err != nil {
		return apperrors.Internal("commit hard delete", err)
	}
	return nil
}

func purgeTenantHard(ctx context.Context, tx pgx.Tx, tenantID string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM ingest_receipts WHERE tenant_id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete ingest receipts", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM copilot_sessions WHERE tenant_id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete tenant copilot sessions", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM copilot_usage WHERE tenant_id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete tenant copilot usage", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM share_links WHERE tenant_id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete share links", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM api_keys WHERE tenant_id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete api keys", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM execution_events
		WHERE execution_id IN (SELECT id FROM executions WHERE tenant_id = $1)
	`, tenantID); err != nil {
		return apperrors.Internal("delete execution events", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM span_causality
		WHERE span_id IN (
			SELECT s.id FROM spans s
			JOIN executions e ON e.id = s.execution_id
			WHERE e.tenant_id = $1
		)
	`, tenantID); err != nil {
		return apperrors.Internal("delete span causality", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM spans
		WHERE execution_id IN (SELECT id FROM executions WHERE tenant_id = $1)
	`, tenantID); err != nil {
		return apperrors.Internal("delete spans", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM executions WHERE tenant_id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete executions", err)
	}

	if err := deleteAuditEventsWhere(ctx, tx, `tenant_id = $1`, tenantID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID); err != nil {
		return apperrors.Internal("delete tenant", err)
	}
	return nil
}

func deleteAuditEventsWhere(ctx context.Context, db pgx.Tx, whereClause string, arg any) error {
	if _, err := db.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER trg_audit_no_delete`); err != nil {
		return apperrors.Internal("disable audit delete guard", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM audit_events WHERE `+whereClause, arg); err != nil {
		return apperrors.Internal("delete audit events", err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER trg_audit_no_delete`); err != nil {
		return apperrors.Internal("enable audit delete guard", err)
	}
	return nil
}
