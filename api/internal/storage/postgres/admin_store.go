package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

// AdminStore serves cross-tenant platform admin queries.
type AdminStore struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewAdminStore constructs an AdminStore.
func NewAdminStore(pool *pgxpool.Pool, logger zerolog.Logger) *AdminStore {
	return &AdminStore{
		pool:   pool,
		logger: logger.With().Str("component", "admin_store").Logger(),
	}
}

// TimeRange is a parsed admin filter window.
type TimeRange struct {
	From time.Time
	To   time.Time
}

// ListParams common pagination + search.
type ListParams struct {
	Range    TimeRange
	Query    string
	Plan     string
	Active   string // "true", "false", ""
	Page     int
	PageSize int
}

func (p ListParams) offset() int {
	if p.Page < 1 {
		return 0
	}
	return (p.Page - 1) * p.limit()
}

func (p ListParams) limit() int {
	if p.PageSize < 1 || p.PageSize > 200 {
		return 50
	}
	return p.PageSize
}

// OverviewStats platform KPI snapshot.
type OverviewStats struct {
	Users         UserOverview           `json:"users"`
	Organizations OrgOverview            `json:"organizations"`
	Tenants       TenantOverview         `json:"tenants"`
	Audit         AuditOverview          `json:"audit"`
	Exports       ExportOverview         `json:"exports"`
	Usage         UsageOverview          `json:"usage"`
	Plans         PlanOverview           `json:"plans"`
	Health        HealthOverview         `json:"health"`
	Charts        map[string][]DayBucket `json:"charts"`
}

type UserOverview struct {
	Total         int64 `json:"total"`
	NewToday      int64 `json:"new_today"`
	New7d         int64 `json:"new_7d"`
	New30d        int64 `json:"new_30d"`
	EmailVerified int64 `json:"email_verified"`
	ZeroOrgs      int64 `json:"zero_orgs"`
	WithOrg       int64 `json:"with_org"`
	DAU           int64 `json:"dau"`
	WAU           int64 `json:"wau"`
	MAU           int64 `json:"mau"`
	Active24h     int64 `json:"active_24h"`
	Active7d      int64 `json:"active_7d"`
	Active30d     int64 `json:"active_30d"`
}

type OrgOverview struct {
	Total              int64 `json:"total"`
	NewToday           int64 `json:"new_today"`
	New7d              int64 `json:"new_7d"`
	New30d             int64 `json:"new_30d"`
	SingleMember       int64 `json:"single_member"`
	MultiMember        int64 `json:"multi_member"`
	PendingInvitations int64 `json:"pending_invitations"`
	WithoutTenant      int64 `json:"without_tenant"`
}

type TenantOverview struct {
	Total       int64        `json:"total"`
	Free        int64        `json:"free"`
	Custom      int64        `json:"custom"`
	Paid        int64        `json:"paid"`
	ZeroEvents  int64        `json:"zero_events"`
	Activated7d int64        `json:"activated_7d"`
	Funnel      FunnelCounts `json:"funnel"`
}

type FunnelCounts struct {
	SignedUp        int64 `json:"signed_up"`
	OrgCreated      int64 `json:"org_created"`
	Bootstrapped    int64 `json:"bootstrapped"`
	HasAPIKey       int64 `json:"has_api_key"`
	FirstAuditEvent int64 `json:"first_audit_event"`
	FirstExecution  int64 `json:"first_execution"`
	FirstExport     int64 `json:"first_export"`
	UsedCopilot     int64 `json:"used_copilot"`
}

type AuditOverview struct {
	TotalAllTime     int64         `json:"total_all_time"`
	Events7d         int64         `json:"events_7d"`
	Events30d        int64         `json:"events_30d"`
	Events90d        int64         `json:"events_90d"`
	EventsInRange    int64         `json:"events_in_range"`
	TotalChainHeight int64         `json:"total_chain_height"`
	ReanchorCount    int64         `json:"reanchor_count"`
	TopActions       []ActionCount `json:"top_actions"`
}

type ActionCount struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

type ExportOverview struct {
	TotalAllTime   int64      `json:"total_all_time"`
	Exports7d      int64      `json:"exports_7d"`
	Exports30d     int64      `json:"exports_30d"`
	ExportsInRange int64      `json:"exports_in_range"`
	SuccessCount   int64      `json:"success_count"`
	FailureCount   int64      `json:"failure_count"`
	LastExportAt   *time.Time `json:"last_export_at,omitempty"`
}

type UsageOverview struct {
	ExecutionsTotal        int64 `json:"executions_total"`
	SpansTotal             int64 `json:"spans_total"`
	Executions7d           int64 `json:"executions_7d"`
	Executions30d          int64 `json:"executions_30d"`
	ExecutionsInRange      int64 `json:"executions_in_range"`
	CopilotSessions        int64 `json:"copilot_sessions"`
	CopilotMessages        int64 `json:"copilot_messages"`
	CopilotToolInvocations int64 `json:"copilot_tool_invocations"`
	APIKeysCreated         int64 `json:"api_keys_created"`
	APIKeysRevoked         int64 `json:"api_keys_revoked"`
}

type PlanOverview struct {
	FreeCount             int64   `json:"free_count"`
	CustomCount           int64   `json:"custom_count"`
	PaidCount             int64   `json:"paid_count"`
	AvgEventsPerTenant    float64 `json:"avg_events_per_tenant"`
	MedianEventsPerTenant float64 `json:"median_events_per_tenant"`
}

type HealthOverview struct {
	BootstrapFailures int64 `json:"bootstrap_failures"`
	JWTAuthErrors     int64 `json:"jwt_auth_errors"`
	SSE401Rate        int64 `json:"sse_401_count"`
	RateLimitHits     int64 `json:"rate_limit_hits"`
	API5xxRate        int64 `json:"api_5xx_count"`
}

type DayBucket struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// AdminUserRow list row for /admin/users.
type AdminUserRow struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	EmailVerified   bool       `json:"email_verified"`
	PlatformRole    *string    `json:"platform_role,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	LastSessionAt   *time.Time `json:"last_session_at,omitempty"`
	OrgCount        int        `json:"org_count"`
	OrgSummary      string     `json:"org_summary"`
	HasBootstrapped bool       `json:"has_bootstrapped"`
	LastActivityAt  *time.Time `json:"last_activity_at,omitempty"`
	CopilotSessions int64      `json:"copilot_sessions"`
}

// AdminTenantRow list row for /admin/tenants.
type AdminTenantRow struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	OrgID           *string    `json:"org_id,omitempty"`
	OrgName         *string    `json:"org_name,omitempty"`
	OwnerEmail      *string    `json:"owner_email,omitempty"`
	PlanID          string     `json:"plan_id"`
	MemberCount     int        `json:"member_count"`
	CreatedAt       time.Time  `json:"created_at"`
	LastActivityAt  *time.Time `json:"last_activity_at,omitempty"`
	AuditTotal      int64      `json:"audit_total"`
	Audit30d        int64      `json:"audit_30d"`
	ChainHeight     int64      `json:"chain_height"`
	KeysActive      int        `json:"keys_active"`
	KeysRevoked     int        `json:"keys_revoked"`
	ExportsCount    int64      `json:"exports_count"`
	ExecutionsCount int64      `json:"executions_count"`
	CopilotSessions int64      `json:"copilot_sessions"`
	OnboardingStep  int        `json:"onboarding_step"`
}

// AdminOrgRow list row for /admin/organizations.
type AdminOrgRow struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	CreatedAt      time.Time  `json:"created_at"`
	MemberCount    int        `json:"member_count"`
	OwnerEmail     *string    `json:"owner_email,omitempty"`
	TenantID       *string    `json:"tenant_id,omitempty"`
	PlanID         *string    `json:"plan_id,omitempty"`
	AuditCount     int64      `json:"audit_count"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
	Bootstrapped   bool       `json:"bootstrapped"`
}

// GetOverview returns platform KPIs for the admin overview page.
func (s *AdminStore) GetOverview(ctx context.Context, tr TimeRange) (*OverviewStats, error) {
	out := &OverviewStats{Charts: map[string][]DayBucket{}}
	now := time.Now().UTC()

	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM "user"),
			(SELECT COUNT(*) FROM "user" WHERE "createdAt" >= $1),
			(SELECT COUNT(*) FROM "user" WHERE "createdAt" >= $2),
			(SELECT COUNT(*) FROM "user" WHERE "createdAt" >= $3),
			(SELECT COUNT(*) FROM "user" WHERE "emailVerified" = true),
			(SELECT COUNT(*) FROM "user" u WHERE NOT EXISTS (
				SELECT 1 FROM "member" m WHERE m."userId" = u.id)),
			(SELECT COUNT(DISTINCT m."userId") FROM "member" m)
	`, now.Add(-24*time.Hour), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&out.Users.Total, &out.Users.NewToday, &out.Users.New7d, &out.Users.New30d,
		&out.Users.EmailVerified, &out.Users.ZeroOrgs, &out.Users.WithOrg,
	); err != nil {
		return nil, apperrors.Internal("admin overview users", err)
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(DISTINCT "userId") FROM "session" WHERE "updatedAt" >= $1),
			(SELECT COUNT(DISTINCT "userId") FROM "session" WHERE "updatedAt" >= $2),
			(SELECT COUNT(DISTINCT "userId") FROM "session" WHERE "updatedAt" >= $3),
			(SELECT COUNT(DISTINCT "userId") FROM "session" WHERE "updatedAt" >= $4),
			(SELECT COUNT(DISTINCT "userId") FROM "session" WHERE "updatedAt" >= $5),
			(SELECT COUNT(DISTINCT "userId") FROM "session" WHERE "updatedAt" >= $6)
	`, now.Add(-24*time.Hour), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour),
		now.Add(-24*time.Hour), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&out.Users.DAU, &out.Users.WAU, &out.Users.MAU,
		&out.Users.Active24h, &out.Users.Active7d, &out.Users.Active30d,
	)

	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM "organization"),
			(SELECT COUNT(*) FROM "organization" WHERE "createdAt" >= $1),
			(SELECT COUNT(*) FROM "organization" WHERE "createdAt" >= $2),
			(SELECT COUNT(*) FROM "organization" WHERE "createdAt" >= $3),
			(SELECT COUNT(*) FROM "invitation" WHERE status = 'pending'),
			(SELECT COUNT(*) FROM "organization" o WHERE NOT EXISTS (
				SELECT 1 FROM tenant_external_identities te
				WHERE te.external_id = o.id AND te.provider = $4))
	`, now.Add(-24*time.Hour), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour),
		audit.ProviderBetterAuthOrg).Scan(
		&out.Organizations.Total, &out.Organizations.NewToday, &out.Organizations.New7d,
		&out.Organizations.New30d, &out.Organizations.PendingInvitations,
		&out.Organizations.WithoutTenant,
	); err != nil {
		return nil, apperrors.Internal("admin overview orgs", err)
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE mc = 1),
			COUNT(*) FILTER (WHERE mc > 1)
		FROM (
			SELECT "organizationId", COUNT(*) AS mc FROM "member" GROUP BY "organizationId"
		) x
	`).Scan(&out.Organizations.SingleMember, &out.Organizations.MultiMember)

	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL),
			(SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL AND plan_id = 'free'),
			(SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL AND plan_id = 'custom'),
			(SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL AND plan_id IN ('pro', 'team')),
			(SELECT COUNT(*) FROM tenants t WHERE deleted_at IS NULL AND NOT EXISTS (
				SELECT 1 FROM audit_events ae WHERE ae.tenant_id = t.id LIMIT 1))
	`).Scan(&out.Tenants.Total, &out.Tenants.Free, &out.Tenants.Custom, &out.Tenants.Paid,
		&out.Tenants.ZeroEvents); err != nil {
		return nil, apperrors.Internal("admin overview tenants", err)
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT tenant_id) FROM audit_events
		WHERE timestamp >= $1
	`, now.Add(-7*24*time.Hour)).Scan(&out.Tenants.Activated7d)

	s.fillFunnel(ctx, &out.Tenants.Funnel)

	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM audit_events),
			(SELECT COUNT(*) FROM audit_events WHERE timestamp >= $1),
			(SELECT COUNT(*) FROM audit_events WHERE timestamp >= $2),
			(SELECT COUNT(*) FROM audit_events WHERE timestamp >= $3),
			COALESCE((SELECT MAX(sequence_number) FROM audit_events), 0),
			(SELECT COUNT(*) FROM audit_events WHERE action = 'chain.reanchored')
	`, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour), now.Add(-90*24*time.Hour)).Scan(
		&out.Audit.TotalAllTime, &out.Audit.Events7d, &out.Audit.Events30d,
		&out.Audit.Events90d, &out.Audit.TotalChainHeight, &out.Audit.ReanchorCount,
	); err != nil {
		return nil, apperrors.Internal("admin overview audit", err)
	}

	if !tr.From.IsZero() {
		_ = s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_events WHERE timestamp >= $1 AND timestamp <= $2`,
			tr.From, tr.To).Scan(&out.Audit.EventsInRange)
	} else {
		out.Audit.EventsInRange = out.Audit.TotalAllTime
	}

	out.Audit.TopActions, _ = s.topActions(ctx, 10)

	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM audit_events WHERE action = 'audit.export'),
			(SELECT COUNT(*) FROM audit_events WHERE action = 'audit.export' AND timestamp >= $1),
			(SELECT COUNT(*) FROM audit_events WHERE action = 'audit.export' AND timestamp >= $2),
			(SELECT COUNT(*) FROM audit_events WHERE action = 'audit.export' AND outcome = 'success'),
			(SELECT COUNT(*) FROM audit_events WHERE action = 'audit.export' AND outcome != 'success'),
			(SELECT MAX(timestamp) FROM audit_events WHERE action = 'audit.export')
	`, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&out.Exports.TotalAllTime, &out.Exports.Exports7d, &out.Exports.Exports30d,
		&out.Exports.SuccessCount, &out.Exports.FailureCount, &out.Exports.LastExportAt,
	); err != nil {
		return nil, apperrors.Internal("admin overview exports", err)
	}

	if !tr.From.IsZero() {
		_ = s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_events WHERE action = 'audit.export' AND timestamp >= $1 AND timestamp <= $2`,
			tr.From, tr.To).Scan(&out.Exports.ExportsInRange)
	} else {
		out.Exports.ExportsInRange = out.Exports.TotalAllTime
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM executions),
			(SELECT COUNT(*) FROM spans),
			(SELECT COUNT(*) FROM executions WHERE started_at >= $1),
			(SELECT COUNT(*) FROM executions WHERE started_at >= $2),
			(SELECT COUNT(*) FROM copilot_sessions),
			(SELECT COUNT(*) FROM copilot_messages),
			(SELECT COUNT(*) FROM copilot_activity WHERE action LIKE 'copilot.tool.%'),
			(SELECT COUNT(*) FROM api_keys),
			(SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NOT NULL)
	`, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&out.Usage.ExecutionsTotal, &out.Usage.SpansTotal,
		&out.Usage.Executions7d, &out.Usage.Executions30d,
		&out.Usage.CopilotSessions, &out.Usage.CopilotMessages,
		&out.Usage.CopilotToolInvocations, &out.Usage.APIKeysCreated, &out.Usage.APIKeysRevoked,
	)

	out.Plans.FreeCount = out.Tenants.Free
	out.Plans.CustomCount = out.Tenants.Custom
	out.Plans.PaidCount = out.Tenants.Paid
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(cnt), 0), COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY cnt), 0)
		FROM (SELECT tenant_id, COUNT(*) AS cnt FROM audit_events GROUP BY tenant_id) x
	`).Scan(&out.Plans.AvgEventsPerTenant, &out.Plans.MedianEventsPerTenant)

	out.Charts["signups"], _ = s.dailyChart(ctx, `
		SELECT DATE("createdAt") AS d, COUNT(*) FROM "user"
		WHERE "createdAt" >= $1 GROUP BY d ORDER BY d
	`, chartSince(tr))
	out.Charts["audit_events"], _ = s.dailyChart(ctx, `
		SELECT DATE(timestamp) AS d, COUNT(*) FROM audit_events
		WHERE timestamp >= $1 GROUP BY d ORDER BY d
	`, chartSince(tr))
	out.Charts["exports"], _ = s.dailyChart(ctx, `
		SELECT DATE(timestamp) AS d, COUNT(*) FROM audit_events
		WHERE action = 'audit.export' AND timestamp >= $1 GROUP BY d ORDER BY d
	`, chartSince(tr))

	return out, nil
}

func chartSince(tr TimeRange) time.Time {
	if !tr.From.IsZero() {
		return tr.From
	}
	return time.Now().UTC().Add(-30 * 24 * time.Hour)
}

func (s *AdminStore) fillFunnel(ctx context.Context, f *FunnelCounts) {
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM "user"`).Scan(&f.SignedUp)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT "userId") FROM "member"`).Scan(&f.OrgCreated)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tenant_external_identities WHERE provider = $1
	`, audit.ProviderBetterAuthOrg).Scan(&f.Bootstrapped)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT tenant_id) FROM api_keys
	`).Scan(&f.HasAPIKey)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT tenant_id) FROM audit_events
	`).Scan(&f.FirstAuditEvent)
	_ = s.pool.QueryRow(ctx, `SELECT CASE WHEN EXISTS (SELECT 1 FROM executions LIMIT 1) THEN 1 ELSE 0 END`).Scan(&f.FirstExecution)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT tenant_id) FROM audit_events WHERE action = 'audit.export'
	`).Scan(&f.FirstExport)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT tenant_id) FROM copilot_sessions
	`).Scan(&f.UsedCopilot)
}

func (s *AdminStore) topActions(ctx context.Context, limit int) ([]ActionCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT action, COUNT(*) AS cnt FROM audit_events
		GROUP BY action ORDER BY cnt DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActionCount
	for rows.Next() {
		var a ActionCount
		if err := rows.Scan(&a.Action, &a.Count); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *AdminStore) dailyChart(ctx context.Context, q string, since time.Time) ([]DayBucket, error) {
	rows, err := s.pool.Query(ctx, q, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayBucket
	for rows.Next() {
		var d time.Time
		var c int64
		if err := rows.Scan(&d, &c); err != nil {
			return nil, err
		}
		out = append(out, DayBucket{Date: d.Format("2006-01-02"), Count: c})
	}
	return out, rows.Err()
}

// ListUsers returns paginated platform users.
func (s *AdminStore) ListUsers(ctx context.Context, p ListParams) ([]AdminUserRow, int64, error) {
	q := `%` + strings.ToLower(strings.TrimSpace(p.Query)) + `%`
	limit, offset := p.limit(), p.offset()

	rows, err := s.pool.Query(ctx, `
		SELECT
			u.id, u.name, u.email, u."emailVerified", u."platformRole", u."createdAt",
			(SELECT MAX(s."updatedAt") FROM "session" s WHERE s."userId" = u.id),
			(SELECT COUNT(*) FROM "member" m WHERE m."userId" = u.id),
			COALESCE((
				SELECT string_agg(o.name || ':' || m.role, ', ' ORDER BY o.name)
				FROM "member" m JOIN "organization" o ON o.id = m."organizationId"
				WHERE m."userId" = u.id
			), ''),
			EXISTS (
				SELECT 1 FROM "member" m
				JOIN tenant_external_identities te ON te.external_id = m."organizationId"
				WHERE m."userId" = u.id AND te.provider = $1
			),
			(SELECT COUNT(*) FROM copilot_sessions cs WHERE cs.user_id = u.id)
		FROM "user" u
		WHERE ($2 = '' OR LOWER(u.email) LIKE $3 OR LOWER(u.name) LIKE $3 OR u.id LIKE $3)
		ORDER BY u."createdAt" DESC
		LIMIT $4 OFFSET $5
	`, audit.ProviderBetterAuthOrg, p.Query, q, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("list admin users", err)
	}
	defer rows.Close()

	var list []AdminUserRow
	for rows.Next() {
		var r AdminUserRow
		var pr *string
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Email, &r.EmailVerified, &pr, &r.CreatedAt,
			&r.LastSessionAt, &r.OrgCount, &r.OrgSummary, &r.HasBootstrapped, &r.CopilotSessions,
		); err != nil {
			return nil, 0, apperrors.Internal("scan admin user", err)
		}
		r.PlatformRole = pr
		list = append(list, r)
	}
	var total int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM "user" u
		WHERE ($1 = '' OR LOWER(u.email) LIKE $2 OR LOWER(u.name) LIKE $2 OR u.id LIKE $2)
	`, p.Query, q).Scan(&total)
	return list, total, rows.Err()
}

// ListTenants returns paginated tenants with aggregates.
func (s *AdminStore) ListTenants(ctx context.Context, p ListParams) ([]AdminTenantRow, int64, error) {
	q := `%` + strings.ToLower(strings.TrimSpace(p.Query)) + `%`
	limit, offset := p.limit(), p.offset()
	planFilter := p.Plan
	if planFilter == "all" {
		planFilter = ""
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			t.id, t.name, te.external_id, o.name,
			(SELECT u.email FROM "member" m
			 JOIN "user" u ON u.id = m."userId"
			 WHERE m."organizationId" = te.external_id AND m.role = 'owner'
			 LIMIT 1),
			t.plan_id, t.created_at,
			(SELECT MAX(ae.timestamp) FROM audit_events ae WHERE ae.tenant_id = t.id),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id AND ae.timestamp >= NOW() - INTERVAL '30 days'),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id),
			(SELECT COUNT(*) FROM api_keys k WHERE k.tenant_id = t.id AND k.revoked_at IS NULL),
			(SELECT COUNT(*) FROM api_keys k WHERE k.tenant_id = t.id AND k.revoked_at IS NOT NULL),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id AND ae.action = 'audit.export'),
			(SELECT COUNT(*) FROM copilot_sessions cs WHERE cs.tenant_id = t.id),
			(SELECT COUNT(*) FROM "member" m WHERE m."organizationId" = te.external_id)
		FROM tenants t
		LEFT JOIN tenant_external_identities te ON te.tenant_id = t.id AND te.provider = $1
		LEFT JOIN "organization" o ON o.id = te.external_id
		WHERE t.deleted_at IS NULL
		  AND ($2 = '' OR t.plan_id = $2)
		  AND ($3 = '' OR LOWER(t.id) LIKE $4 OR LOWER(t.name) LIKE $4 OR LOWER(COALESCE(o.name,'')) LIKE $4)
		ORDER BY t.created_at DESC
		LIMIT $5 OFFSET $6
	`, audit.ProviderBetterAuthOrg, planFilter, p.Query, q, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("list admin tenants", err)
	}
	defer rows.Close()

	var list []AdminTenantRow
	for rows.Next() {
		var r AdminTenantRow
		var orgID, orgName, ownerEmail *string
		if err := rows.Scan(
			&r.ID, &r.Name, &orgID, &orgName, &ownerEmail, &r.PlanID, &r.CreatedAt,
			&r.LastActivityAt, &r.AuditTotal, &r.Audit30d, &r.ChainHeight,
			&r.KeysActive, &r.KeysRevoked, &r.ExportsCount, &r.CopilotSessions, &r.MemberCount,
		); err != nil {
			return nil, 0, apperrors.Internal("scan admin tenant", err)
		}
		r.OrgID = orgID
		r.OrgName = orgName
		r.OwnerEmail = ownerEmail
		r.OnboardingStep = onboardingStep(r.AuditTotal, r.KeysActive)
		list = append(list, r)
	}

	var total int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tenants t
		LEFT JOIN tenant_external_identities te ON te.tenant_id = t.id AND te.provider = $1
		LEFT JOIN "organization" o ON o.id = te.external_id
		WHERE t.deleted_at IS NULL
		  AND ($2 = '' OR t.plan_id = $2)
		  AND ($3 = '' OR LOWER(t.id) LIKE $4 OR LOWER(t.name) LIKE $4 OR LOWER(COALESCE(o.name,'')) LIKE $4)
	`, audit.ProviderBetterAuthOrg, planFilter, p.Query, q).Scan(&total)
	return list, total, rows.Err()
}

func onboardingStep(events int64, keysActive int) int {
	if events > 0 {
		return 0
	}
	if keysActive > 0 {
		return 3
	}
	return 2
}

// ListOrganizations returns paginated orgs.
func (s *AdminStore) ListOrganizations(ctx context.Context, p ListParams) ([]AdminOrgRow, int64, error) {
	q := `%` + strings.ToLower(strings.TrimSpace(p.Query)) + `%`
	limit, offset := p.limit(), p.offset()

	rows, err := s.pool.Query(ctx, `
		SELECT
			o.id, o.name, o.slug, o."createdAt",
			(SELECT COUNT(*) FROM "member" m WHERE m."organizationId" = o.id),
			(SELECT u.email FROM "member" m JOIN "user" u ON u.id = m."userId"
			 WHERE m."organizationId" = o.id AND m.role = 'owner' LIMIT 1),
			te.tenant_id,
			(SELECT t.plan_id FROM tenants t WHERE t.id = te.tenant_id),
			COALESCE((SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = te.tenant_id), 0),
			(SELECT MAX(ae.timestamp) FROM audit_events ae WHERE ae.tenant_id = te.tenant_id),
			(te.tenant_id IS NOT NULL)
		FROM "organization" o
		LEFT JOIN tenant_external_identities te ON te.external_id = o.id AND te.provider = $1
		WHERE ($2 = '' OR LOWER(o.name) LIKE $3 OR LOWER(o.slug) LIKE $3 OR o.id LIKE $3)
		ORDER BY o."createdAt" DESC
		LIMIT $4 OFFSET $5
	`, audit.ProviderBetterAuthOrg, p.Query, q, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("list admin orgs", err)
	}
	defer rows.Close()

	var list []AdminOrgRow
	for rows.Next() {
		var r AdminOrgRow
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Slug, &r.CreatedAt, &r.MemberCount, &r.OwnerEmail,
			&r.TenantID, &r.PlanID, &r.AuditCount, &r.LastActivityAt, &r.Bootstrapped,
		); err != nil {
			return nil, 0, apperrors.Internal("scan admin org", err)
		}
		list = append(list, r)
	}
	var total int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM "organization" o
		WHERE ($1 = '' OR LOWER(o.name) LIKE $2 OR LOWER(o.slug) LIKE $2 OR o.id LIKE $2)
	`, p.Query, q).Scan(&total)
	return list, total, rows.Err()
}

// ListActivity returns global audit events with filters.
func (s *AdminStore) ListActivity(ctx context.Context, p ListParams, tenantID, action, actorType, outcome string) ([]audit.AuditEvent, int64, error) {
	limit, offset := p.limit(), p.offset()
	q := `%` + strings.ToLower(strings.TrimSpace(p.Query)) + `%`

	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, timestamp, actor_id, actor_type, actor_email,
		       action, resource_id, resource_type, resource_name, outcome,
		       metadata, prev_hash, hash, sequence_number
		FROM audit_events
		WHERE ($1 = '' OR tenant_id = $1)
		  AND ($2 = '' OR action = $2 OR action LIKE $2 || '.%')
		  AND ($3 = '' OR actor_type = $3)
		  AND ($4 = '' OR outcome = $4)
		  AND ($5 = '' OR LOWER(action) LIKE $6 OR LOWER(actor_id) LIKE $6 OR id LIKE $6)
		ORDER BY timestamp DESC
		LIMIT $7 OFFSET $8
	`, tenantID, action, actorType, outcome, p.Query, q, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("list admin activity", err)
	}
	defer rows.Close()

	var events []*audit.AuditEvent
	for rows.Next() {
		e, err := scanAuditEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}

	var total int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE ($1 = '' OR tenant_id = $1)
		  AND ($2 = '' OR action = $2 OR action LIKE $2 || '.%')
		  AND ($3 = '' OR actor_type = $3)
		  AND ($4 = '' OR outcome = $4)
		  AND ($5 = '' OR LOWER(action) LIKE $6 OR LOWER(actor_id) LIKE $6 OR id LIKE $6)
	`, tenantID, action, actorType, outcome, p.Query, q).Scan(&total)

	out := make([]audit.AuditEvent, len(events))
	for i, e := range events {
		out[i] = *e
	}
	return out, total, rows.Err()
}

// ListExports returns audit.export events.
func (s *AdminStore) ListExports(ctx context.Context, p ListParams) ([]audit.AuditEvent, int64, error) {
	return s.ListActivity(ctx, p, "", "audit.export", "", "")
}

// SetPlatformRole updates a user's global platform role (NULL to remove).
func (s *AdminStore) SetPlatformRole(ctx context.Context, userID, role string) error {
	var val *string
	if role != "" {
		val = &role
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE "user" SET "platformRole" = $2, "updatedAt" = NOW() WHERE id = $1
	`, userID, val)
	if err != nil {
		return apperrors.Internal("set platform role", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("user", userID)
	}
	return nil
}

// SetTenantPlan updates tenants.plan_id.
func (s *AdminStore) SetTenantPlan(ctx context.Context, tenantID, planID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tenants
		SET plan_id = $2,
		    subscription_status = NULL,
		    subscription_current_period_end = NULL,
		    stripe_subscription_id = NULL
		WHERE id = $1 AND deleted_at IS NULL
	`, tenantID, planID)
	if err != nil {
		return apperrors.Internal("set tenant plan", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("tenant", tenantID)
	}
	return nil
}

// SetTenantPlanPromo updates tenants.plan_id to a promotional plan with an expiration.
func (s *AdminStore) SetTenantPlanPromo(ctx context.Context, tenantID, planID string, expiresAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tenants
		SET plan_id = $2,
		    subscription_status = 'active',
		    subscription_current_period_end = $3,
		    stripe_subscription_id = 'promo_pro_1month_' || id,
		    trial_reminder_sent = false,
		    trial_expired_email_sent = false
		WHERE id = $1 AND deleted_at IS NULL
	`, tenantID, planID, expiresAt)
	if err != nil {
		return apperrors.Internal("set tenant plan promo", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("tenant", tenantID)
	}
	return nil
}

// GetTenantOwnerInfo queries the owner email, name, and workspace name for a given tenant.
func (s *AdminStore) GetTenantOwnerInfo(ctx context.Context, tenantID string) (email, ownerName, tenantName string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(u.email, ''),
			COALESCE(u.name, ''),
			COALESCE(t.name, '')
		FROM tenants t
		LEFT JOIN tenant_external_identities te ON te.tenant_id = t.id AND te.provider = 'better_auth_org'
		LEFT JOIN "member" m ON m."organizationId" = te.external_id AND m.role = 'owner'
		LEFT JOIN "user" u ON u.id = m."userId"
		WHERE t.id = $1 AND t.deleted_at IS NULL
		ORDER BY u.email NULLS LAST
		LIMIT 1
	`, tenantID).Scan(&email, &ownerName, &tenantName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", nil
		}
		return "", "", "", apperrors.Internal("get tenant owner info", err)
	}
	return
}

// GetTenantDetail returns tenant detail for admin view.
func (s *AdminStore) GetTenantDetail(ctx context.Context, tenantID string) (*AdminTenantRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			t.id, t.name, te.external_id, o.name,
			(SELECT u.email FROM "member" m
			 JOIN "user" u ON u.id = m."userId"
			 WHERE m."organizationId" = te.external_id AND m.role = 'owner'
			 LIMIT 1),
			t.plan_id, t.created_at,
			(SELECT MAX(ae.timestamp) FROM audit_events ae WHERE ae.tenant_id = t.id),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id AND ae.timestamp >= NOW() - INTERVAL '30 days'),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id),
			(SELECT COUNT(*) FROM api_keys k WHERE k.tenant_id = t.id AND k.revoked_at IS NULL),
			(SELECT COUNT(*) FROM api_keys k WHERE k.tenant_id = t.id AND k.revoked_at IS NOT NULL),
			(SELECT COUNT(*) FROM audit_events ae WHERE ae.tenant_id = t.id AND ae.action = 'audit.export'),
			(SELECT COUNT(*) FROM copilot_sessions cs WHERE cs.tenant_id = t.id),
			(SELECT COUNT(*) FROM "member" m WHERE m."organizationId" = te.external_id)
		FROM tenants t
		LEFT JOIN tenant_external_identities te ON te.tenant_id = t.id AND te.provider = $1
		LEFT JOIN "organization" o ON o.id = te.external_id
		WHERE t.id = $2 AND t.deleted_at IS NULL
	`, audit.ProviderBetterAuthOrg, tenantID)

	var r AdminTenantRow
	var orgID, orgName, ownerEmail *string
	err := row.Scan(
		&r.ID, &r.Name, &orgID, &orgName, &ownerEmail, &r.PlanID, &r.CreatedAt,
		&r.LastActivityAt, &r.AuditTotal, &r.Audit30d, &r.ChainHeight,
		&r.KeysActive, &r.KeysRevoked, &r.ExportsCount, &r.CopilotSessions, &r.MemberCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.NotFound("tenant", tenantID)
		}
		return nil, apperrors.Internal("get tenant detail", err)
	}
	r.OrgID = orgID
	r.OrgName = orgName
	r.OwnerEmail = ownerEmail
	r.OnboardingStep = onboardingStep(r.AuditTotal, r.KeysActive)
	return &r, nil
}

// GetUserDetail loads user + org memberships.
func (s *AdminStore) GetUserDetail(ctx context.Context, userID string) (map[string]interface{}, error) {
	var name, email string
	var verified bool
	var platformRole *string
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT name, email, "emailVerified", "platformRole", "createdAt"
		FROM "user" WHERE id = $1
	`, userID).Scan(&name, &email, &verified, &platformRole, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.NotFound("user", userID)
	}
	if err != nil {
		return nil, apperrors.Internal("get user detail", err)
	}

	type memberRow struct {
		OrgID    string  `json:"org_id"`
		OrgName  string  `json:"org_name"`
		Role     string  `json:"role"`
		TenantID *string `json:"tenant_id,omitempty"`
	}
	memberRows, _ := s.pool.Query(ctx, `
		SELECT m."organizationId", o.name, m.role, te.tenant_id
		FROM "member" m
		JOIN "organization" o ON o.id = m."organizationId"
		LEFT JOIN tenant_external_identities te ON te.external_id = o.id AND te.provider = $2
		WHERE m."userId" = $1
	`, userID, audit.ProviderBetterAuthOrg)
	defer memberRows.Close()
	var memberships []memberRow
	for memberRows.Next() {
		var m memberRow
		_ = memberRows.Scan(&m.OrgID, &m.OrgName, &m.Role, &m.TenantID)
		memberships = append(memberships, m)
	}

	return map[string]interface{}{
		"id":             userID,
		"name":           name,
		"email":          email,
		"email_verified": verified,
		"platform_role":  platformRole,
		"created_at":     createdAt,
		"memberships":    memberships,
	}, nil
}

// CopilotStats platform copilot aggregates.
func (s *AdminStore) CopilotStats(ctx context.Context) (map[string]interface{}, error) {
	var sessions, messages, tools int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM copilot_sessions`).Scan(&sessions)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM copilot_messages`).Scan(&messages)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM copilot_activity WHERE action LIKE 'copilot.tool.%'
	`).Scan(&tools)

	topTools, _ := s.topActionsFiltered(ctx, "copilot.tool.", 10)
	topTenants, _ := s.pool.Query(ctx, `
		SELECT tenant_id, COUNT(*) AS cnt FROM copilot_sessions
		GROUP BY tenant_id ORDER BY cnt DESC LIMIT 10
	`)
	defer topTenants.Close()
	type tenantCount struct {
		TenantID string `json:"tenant_id"`
		Count    int64  `json:"count"`
	}
	var tt []tenantCount
	for topTenants.Next() {
		var t tenantCount
		_ = topTenants.Scan(&t.TenantID, &t.Count)
		tt = append(tt, t)
	}

	return map[string]interface{}{
		"total_sessions":   sessions,
		"total_messages":   messages,
		"tool_invocations": tools,
		"top_tools":        topTools,
		"top_tenants":      tt,
	}, nil
}

func (s *AdminStore) topActionsFiltered(ctx context.Context, prefix string, limit int) ([]ActionCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT action, COUNT(*) FROM audit_events
		WHERE action LIKE $1 || '%' GROUP BY action ORDER BY COUNT(*) DESC LIMIT $2
	`, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActionCount
	for rows.Next() {
		var a ActionCount
		_ = rows.Scan(&a.Action, &a.Count)
		out = append(out, a)
	}
	return out, rows.Err()
}

// HealthStats returns platform health snapshot.
func (s *AdminStore) HealthStats(ctx context.Context) (map[string]interface{}, error) {
	var zeroEvents, reanchors int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tenants t WHERE deleted_at IS NULL AND NOT EXISTS (
			SELECT 1 FROM audit_events ae WHERE ae.tenant_id = t.id)
	`).Scan(&zeroEvents)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events WHERE action = 'chain.reanchored'
	`).Scan(&reanchors)

	type stuckTenant struct {
		ID   string `json:"tenant_id"`
		Name string `json:"name"`
	}
	rows, _ := s.pool.Query(ctx, `
		SELECT t.id, t.name FROM tenants t
		WHERE t.deleted_at IS NULL AND NOT EXISTS (
			SELECT 1 FROM audit_events ae WHERE ae.tenant_id = t.id)
		ORDER BY t.created_at DESC LIMIT 20
	`)
	defer rows.Close()
	var stuck []stuckTenant
	for rows.Next() {
		var s stuckTenant
		_ = rows.Scan(&s.ID, &s.Name)
		stuck = append(stuck, s)
	}

	return map[string]interface{}{
		"setup_stuck_count":   zeroEvents,
		"setup_stuck_tenants": stuck,
		"reanchor_count":      reanchors,
		"jwt_auth_errors":     auth.JWTVerifyFailures(),
		"sse_401_count":       auth.SSETicketFailures(),
	}, nil
}

func scanAuditEvent(rows pgx.Rows) (*audit.AuditEvent, error) {
	var e audit.AuditEvent
	var meta []byte
	var seq int64
	if err := rows.Scan(
		&e.ID, &e.TenantID, &e.Timestamp, &e.Actor.ID, &e.Actor.Type, &e.Actor.Email,
		&e.Action, &e.Resource.ID, &e.Resource.Type, &e.Resource.Name, &e.Outcome,
		&meta, &e.PrevHash, &e.Hash, &seq,
	); err != nil {
		return nil, apperrors.Internal("scan audit event", err)
	}
	e.SequenceNumber = seq
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &e.Metadata)
	}
	return &e, nil
}

type AdminTransactionRow struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	TenantName       string    `json:"tenant_name"`
	OrganizationName string    `json:"organization_name"`
	OwnerEmail       string    `json:"owner_email"`
	AmountCents      int64     `json:"amount_cents"`
	Currency         string    `json:"currency"`
	Status           string    `json:"status"`
	StripeInvoiceID  string    `json:"stripe_invoice_id"`
	HostedInvoiceURL string    `json:"hosted_invoice_url"`
	InvoicePDF       string    `json:"invoice_pdf"`
	CreatedAt        time.Time `json:"created_at"`
}

// ListTransactions retrieves global paginated transactions for admin overview.
func (s *AdminStore) ListTransactions(ctx context.Context, p ListParams) ([]AdminTransactionRow, int64, error) {
	limit, offset := p.limit(), p.offset()

	rows, err := s.pool.Query(ctx, `
		SELECT
			tx.id,
			tx.tenant_id,
			t.name AS tenant_name,
			COALESCE(o.name, '') AS organization_name,
			COALESCE((
				SELECT u.email FROM "member" m
				JOIN "user" u ON u.id = m."userId"
				WHERE m."organizationId" = te.external_id AND m.role = 'owner'
				LIMIT 1
			), '') AS owner_email,
			tx.amount_cents,
			tx.currency,
			tx.status,
			COALESCE(tx.stripe_invoice_id, ''),
			COALESCE(tx.hosted_invoice_url, ''),
			COALESCE(tx.invoice_pdf, ''),
			tx.created_at
		FROM transactions tx
		JOIN tenants t ON t.id = tx.tenant_id
		LEFT JOIN tenant_external_identities te ON te.tenant_id = tx.tenant_id AND te.provider = $1
		LEFT JOIN "organization" o ON o.id = te.external_id
		ORDER BY tx.created_at DESC
		LIMIT $2 OFFSET $3
	`, audit.ProviderBetterAuthOrg, limit, offset)
	if err != nil {
		return nil, 0, apperrors.Internal("list admin transactions", err)
	}
	defer rows.Close()

	var list []AdminTransactionRow
	for rows.Next() {
		var r AdminTransactionRow
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.TenantName, &r.OrganizationName, &r.OwnerEmail,
			&r.AmountCents, &r.Currency, &r.Status, &r.StripeInvoiceID,
			&r.HostedInvoiceURL, &r.InvoicePDF, &r.CreatedAt,
		); err != nil {
			return nil, 0, apperrors.Internal("scan admin transaction", err)
		}
		list = append(list, r)
	}

	var total int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions`).Scan(&total)

	return list, total, rows.Err()
}
