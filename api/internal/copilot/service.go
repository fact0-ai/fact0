package copilot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	apperrors "github.com/fact0-ai/fact0/internal/platform/errors"
)

const (
	maxTitleLen      = 120
	maxMetadataField = 512
)

var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(rand.Reader, 0)
)

// NewSessionID returns a copilot session identifier.
func NewSessionID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "cps_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewMessageID returns a copilot message identifier.
func NewMessageID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "cpm_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewActivityID returns a copilot activity row identifier.
func NewActivityID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return "cpa_" + ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// Principal identifies the authenticated dashboard user.
type Principal struct {
	UserID string
	OrgID  string
}

// Service owns copilot session/message persistence and activity telemetry.
type Service struct {
	repo       Repository
	plans      PlanStore
	logger     zerolog.Logger
	sessionTTL time.Duration
}

// PlanStore is a minimal interface to read the plan for a tenant.
// It mirrors audit.PlanStore but is declared here to avoid a dependency
// cycle between copilot and audit packages.
type PlanStore interface {
	GetTenantPlan(ctx context.Context, tenantID string) (*Plan, error)
}

// Plan mirrors the fields we need from audit.Plan.
type Plan struct {
	ID                  string `json:"id"`
	MonthlyCopilotLimit *int   `json:"monthly_copilot_limit,omitempty"`
}

// NewService constructs a copilot Service.
func NewService(repo Repository, plans PlanStore, sessionTTLDays int, logger zerolog.Logger) *Service {
	if sessionTTLDays <= 0 {
		sessionTTLDays = 7
	}
	return &Service{
		repo:       repo,
		plans:      plans,
		logger:     logger.With().Str("module", "copilot").Logger(),
		sessionTTL: time.Duration(sessionTTLDays) * 24 * time.Hour,
	}
}

// SessionTTL returns the resume window for copilot sessions.
func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }

// CreateSession mints a new session and records copilot.session.started.
func (s *Service) CreateSession(ctx context.Context, p Principal, tenantID string) (*Session, error) {
	if p.UserID == "" || p.OrgID == "" {
		return nil, apperrors.InvalidInput("user and org required")
	}
	if tenantID == "" {
		return nil, apperrors.InvalidInput("tenant_id required")
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:        NewSessionID(),
		TenantID:  tenantID,
		UserID:    p.UserID,
		OrgID:     p.OrgID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	if err := s.logActivity(ctx, tenantID, p, ActivityInput{
		Action:    ActionSessionStarted,
		SessionID: sess.ID,
		Outcome:   "success",
		ActorType: "human",
	}); err != nil {
		s.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("copilot activity log failed")
	}
	return sess, nil
}

// GetSession returns a session if it belongs to the caller and is within
// the resume window.
func (s *Service) GetSession(ctx context.Context, p Principal, tenantID, sessionID string) (*Session, error) {
	sess, err := s.repo.GetSession(ctx, tenantID, p.UserID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.assertResumeWindow(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// ListSessions returns sessions updated within the resume window.
func (s *Service) ListSessions(ctx context.Context, p Principal, tenantID string) ([]*Session, error) {
	since := time.Now().UTC().Add(-s.sessionTTL)
	return s.repo.ListSessions(ctx, tenantID, p.UserID, since)
}

// AppendMessages validates ownership and appends transcript rows.
func (s *Service) AppendMessages(
	ctx context.Context,
	p Principal,
	tenantID, sessionID string,
	msgs []MessageInput,
) error {
	if len(msgs) == 0 {
		return apperrors.InvalidInput("messages required")
	}
	sess, err := s.repo.GetSession(ctx, tenantID, p.UserID, sessionID)
	if err != nil {
		return err
	}
	if err := s.assertResumeWindow(sess); err != nil {
		return err
	}
	for i := range msgs {
		if msgs[i].ID == "" {
			msgs[i].ID = NewMessageID()
		}
		msgs[i].Role = strings.TrimSpace(msgs[i].Role)
		if msgs[i].Role != "user" && msgs[i].Role != "assistant" && msgs[i].Role != "tool" {
			return apperrors.Validation("role", "must be user|assistant|tool")
		}
	}

	title := sess.Title
	for _, m := range msgs {
		if title == "" && m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			title = truncate(m.Content, maxTitleLen)
			break
		}
	}
	if err := s.repo.AppendMessages(ctx, tenantID, sessionID, msgs); err != nil {
		return err
	}
	return s.repo.TouchSession(ctx, tenantID, p.UserID, sessionID, title)
}

// ListMessages returns transcript rows for a resumable session.
func (s *Service) ListMessages(ctx context.Context, p Principal, tenantID, sessionID string) ([]*Message, error) {
	sess, err := s.repo.GetSession(ctx, tenantID, p.UserID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.assertResumeWindow(sess); err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, tenantID, p.UserID, sessionID)
}

// LogActivity writes metadata-only copilot telemetry to copilot_activity.
func (s *Service) LogActivity(ctx context.Context, p Principal, tenantID string, in ActivityInput) error {
	if in.SessionID != "" {
		if _, err := s.repo.GetSession(ctx, tenantID, p.UserID, in.SessionID); err != nil {
			return err
		}
	}
	return s.logActivity(ctx, tenantID, p, in)
}

// LogAudit is an alias for LogActivity (handler compat).
func (s *Service) LogAudit(ctx context.Context, p Principal, tenantID string, in AuditInput) error {
	return s.LogActivity(ctx, p, tenantID, in)
}

func (s *Service) assertResumeWindow(sess *Session) error {
	cutoff := time.Now().UTC().Add(-s.sessionTTL)
	if sess.UpdatedAt.Before(cutoff) {
		return apperrors.NotFound("copilot_session", sess.ID)
	}
	return nil
}

func (s *Service) logActivity(ctx context.Context, tenantID string, p Principal, in ActivityInput) error {
	if in.Action == "" {
		return apperrors.Validation("action", "must not be empty")
	}
	if in.SessionID == "" {
		return apperrors.Validation("session_id", "must not be empty")
	}

	actorType := in.ActorType
	if actorType == "" {
		actorType = "human"
	}
	switch actorType {
	case "human", "agent", "system":
	default:
		return apperrors.Validation("actor_type", "must be human|agent|system")
	}

	outcome := in.Outcome
	if outcome == "" {
		outcome = "success"
	}
	switch outcome {
	case "success", "failure", "error":
	default:
		return apperrors.Validation("outcome", "must be success|failure|error")
	}

	meta := sanitizeMetadata(in.Metadata)
	if in.Action == ActionQuerySubmitted {
		if _, ok := meta["content_hash"]; !ok {
			if raw, ok := meta["content"]; ok {
				if str, ok := raw.(string); ok {
					meta["content_hash"] = hashContent(str)
					delete(meta, "content")
				}
			}
		}
	}
	meta["session_id"] = in.SessionID

	return s.repo.InsertActivity(ctx, &Activity{
		ID:        NewActivityID(),
		TenantID:  tenantID,
		SessionID: in.SessionID,
		UserID:    p.UserID,
		Action:    in.Action,
		ActorType: actorType,
		Outcome:   outcome,
		Metadata:  meta,
		CreatedAt: time.Now().UTC(),
	})
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func sanitizeMetadata(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		switch x := v.(type) {
		case string:
			out[k] = truncate(x, maxMetadataField)
		default:
			out[k] = v
		}
	}
	return out
}

// currentPeriod returns the YYYY-MM string for UTC now.
func currentPeriod() string {
	return time.Now().UTC().Format("2006-01")
}

// nextPeriodStart returns the first day of the next UTC month.
func nextPeriodStart() time.Time {
	now := time.Now().UTC()
	y, m, _ := now.Date()
	return time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
}

// CheckQuota returns the current quota status for a user.
// If plans store is nil or the plan has no copilot limit, it returns unlimited.
func (s *Service) CheckQuota(ctx context.Context, p Principal, tenantID string) (*CopilotQuota, error) {
	period := currentPeriod()
	used, err := s.repo.GetUsage(ctx, tenantID, p.UserID, period)
	if err != nil {
		return nil, err
	}

	quota := &CopilotQuota{
		Used:     used,
		ResetsAt: nextPeriodStart().Format(time.RFC3339),
		Plan:     "free",
	}

	if s.plans != nil {
		plan, err := s.plans.GetTenantPlan(ctx, tenantID)
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to fetch plan for copilot quota")
			// Graceful degradation: allow usage if we can't determine the plan.
			return quota, nil
		}
		quota.Plan = plan.ID
		quota.Limit = plan.MonthlyCopilotLimit
	}

	if quota.Plan == "free" {
		six := 6
		quota.Limit = &six
	}

	return quota, nil
}

// IncrementUsage bumps the message count for the current month.
// Returns the new count. Does NOT enforce the limit - the caller
// (chat route handler) should call CheckQuota first.
func (s *Service) IncrementUsage(ctx context.Context, p Principal, tenantID string) (int, error) {
	return s.repo.IncrementUsage(ctx, tenantID, p.UserID, currentPeriod())
}
