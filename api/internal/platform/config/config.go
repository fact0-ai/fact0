// Package config provides application configuration via environment variables.
package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sethvargo/go-envconfig"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig   `env:", prefix=FACT0_"`
	Postgres PostgresConfig `env:", prefix=FACT0_POSTGRES_"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	// OSSMode enables the supported single-owner, synchronous local profile.
	OSSMode  bool   `env:"OSS_MODE, default=true"`
	Port     int    `env:"PORT, default=8000"`
	LogLevel string `env:"LOG_LEVEL, default=info"`
	// LogFormat controls the log output format.
	// "json"    → structured JSON (CloudWatch / ECS / production)
	// "console" → coloured human output
	// "auto"    → JSON when not a TTY, console otherwise (default)
	LogFormat string `env:"LOG_FORMAT, default=auto"`

	// BetterAuthJWKSURL is the absolute URL of the dashboard's Better
	// Auth JWKS endpoint, typically `${BETTER_AUTH_URL}/api/auth/jwks`.
	// Empty disables the /v1/me/* routes (returns 404), which is useful
	// for local dev without the dashboard running.
	BetterAuthJWKSURL string `env:"BETTER_AUTH_JWKS_URL"`

	// BetterAuthIssuer is the optional `iss` claim allowlist. When set,
	// JWTs whose issuer differs are rejected. Typically equals
	// `${BETTER_AUTH_URL}/api/auth`.
	BetterAuthIssuer string `env:"BETTER_AUTH_ISSUER"`

	// BetterAuthAuthorizedParties is the optional `azp` claim allowlist
	// (comma-separated origins). When set, JWTs whose authorized party
	// isn't in this list are rejected.
	// Production example: https://fact0.io,https://staging.fact0.io
	BetterAuthAuthorizedParties []string `env:"BETTER_AUTH_AUTHORIZED_PARTIES"`

	// AuthWebhookSecret is the shared HMAC-SHA256 secret used by the
	// dashboard's databaseHooks bridge to sign POSTs to /webhooks/auth.
	// Empty disables the webhook route (returns 404). Required for
	// organization.deleted / organization.updated sync.
	AuthWebhookSecret string `env:"AUTH_WEBHOOK_SECRET"`

	// RazorpayKeyID is the key ID provided by Razorpay.
	RazorpayKeyID string `env:"RAZORPAY_KEY_ID"`

	// RazorpayKeySecret is the key secret provided by Razorpay.
	RazorpayKeySecret string `env:"RAZORPAY_KEY_SECRET"`

	// RazorpayWebhookSecret is the webhook signature secret from Razorpay.
	RazorpayWebhookSecret string `env:"RAZORPAY_WEBHOOK_SECRET"`

	// RazorpayPlanIDPro is the Razorpay Plan ID for the Pro subscription.
	RazorpayPlanIDPro string `env:"RAZORPAY_PLAN_ID_PRO"`

	// RazorpayPlanIDTeam is the Razorpay Plan ID for the Team subscription.
	RazorpayPlanIDTeam string `env:"RAZORPAY_PLAN_ID_TEAM"`

	// CORSAllowedOrigins is the list of origins allowed to make
	// cross-origin requests (e.g. https://app.example.com).
	// When empty the server allows ALL origins ("*") - fine for local
	// dev but must be set in production to prevent data leakage.
	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS"`

	// SigningKey is the base64 or hex encoded Ed25519 private key for signing exports
	SigningKey string `env:"SIGNING_KEY"`

	// ─── Email bridge (Go → Next.js Resend) ───────────────────────
	EmailBridgeURL     string `env:"EMAIL_BRIDGE_URL"`
	EmailWebhookSecret string `env:"EMAIL_WEBHOOK_SECRET"`

	// ─── Ingest rate limiting ──────────────────────────────────
	// In-memory token-bucket limiter applied per api_key_id (audit
	// writes) or per source IP (anonymous telemetry). See
	// internal/transport/rest/ratelimit.go for the algorithm.
	// Set RateLimitDisabled=true to bypass entirely (useful for tests
	// and benchmarks).
	RateLimitDisabled         bool `env:"RATELIMIT_DISABLED, default=false"`
	RateLimitEventsPerSec     int  `env:"RATELIMIT_EVENTS_PER_SEC, default=50"`
	RateLimitEventsBurst      int  `env:"RATELIMIT_BURST, default=200"`
	RateLimitExecutionsPerMin int  `env:"RATELIMIT_EXECUTIONS_PER_MIN, default=120"`

	// RedactionEnabled toggles ingest-time PII/secret scrubbing.
	// Defaults to false for full local capture. Operators can explicitly
	// enable scrubbing, in which case stored values are redacted before hashing.
	RedactionEnabled bool `env:"REDACTION_ENABLED, default=false"`

	// ─── Retention sweeper ─────────────────────────────────────
	// Background goroutine that enforces per-plan data retention.
	// See internal/retention/sweeper.go. Disable in tests via
	// RetentionDisabled=true; production should always run it.
	RetentionDisabled     bool          `env:"RETENTION_DISABLED, default=false"`
	RetentionInterval     time.Duration `env:"RETENTION_INTERVAL, default=1h"`
	RetentionBatchSize    int           `env:"RETENTION_BATCH_SIZE, default=5000"`
	RetentionFallbackDays int           `env:"RETENTION_FALLBACK_DAYS, default=30"`
	// FreeMonthlyEventLimit overrides the free plan monthly audit cap (default 30000).
	FreeMonthlyEventLimit int64 `env:"FREE_MONTHLY_EVENT_LIMIT, default=30000"`
	// CopilotSessionTTLDays is the resume window for dashboard copilot threads.
	CopilotSessionTTLDays int `env:"COPILOT_SESSION_TTL_DAYS, default=7"`

	// ─── Coding agents weekly digest ───────────────────────────
	// Weekly "your AI coding week" summary via tenant alert channels.
	// See internal/digest/worker.go.
	DigestDisabled bool          `env:"DIGEST_DISABLED, default=false"`
	DigestInterval time.Duration `env:"DIGEST_INTERVAL, default=1h"`

	// ─── HTTP server timeouts ──────────────────────────────────
	ReadTimeout  time.Duration `env:"READ_TIMEOUT, default=15s"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT, default=60s"`
	IdleTimeout  time.Duration `env:"IDLE_TIMEOUT, default=60s"`

	// MaxBatchBodyBytes caps the request body size for audit batch ingest.
	MaxBatchBodyBytes int64 `env:"MAX_BATCH_BODY_BYTES, default=4194304"`

	// AuthKeyCacheTTL controls how long resolved API keys are cached in-process.
	AuthKeyCacheTTL time.Duration `env:"AUTH_KEY_CACHE_TTL, default=60s"`

	// ─── Rate limit backend ────────────────────────────────────
	// RateLimitBackend is "memory" (default) or "redis".
	RateLimitBackend string `env:"RATELIMIT_BACKEND, default=memory"`
	RedisURL         string `env:"REDIS_URL"`

	// ─── Async ingest (Phase 2) ─────────────────────────────────
	IngestWorkerEnabled      bool          `env:"INGEST_WORKER_ENABLED, default=true"`
	IngestAsyncDefault       bool          `env:"INGEST_ASYNC_DEFAULT, default=true"`
	IngestWorkerPollInterval time.Duration `env:"INGEST_WORKER_POLL_INTERVAL, default=200ms"`
	IngestWorkerBatchSize    int           `env:"INGEST_WORKER_BATCH_SIZE, default=32"`
	IngestWorkerMaxAttempts  int           `env:"INGEST_WORKER_MAX_ATTEMPTS, default=10"`

	// ─── OTLP receiver (OpenTelemetry native ingestion) ─────
	// When enabled, mounts an OTLP/HTTP endpoint at /v1/otlp/v1/traces
	// that accepts ExportTraceServiceRequest from any OTel SDK or collector.
	OTLPHTTPEnabled bool `env:"OTLP_HTTP_ENABLED, default=false"`

	// ─── OTLP gRPC receiver ─────────────────────────────────
	// When enabled, starts a separate gRPC listener on OTLPGRPCPort
	// implementing the OTLP TraceService. This is the default transport
	// for most OTel SDKs and collectors.
	OTLPGRPCEnabled bool `env:"OTLP_GRPC_ENABLED, default=false"`
	OTLPGRPCPort    int  `env:"OTLP_GRPC_PORT, default=4317"`

	// ─── External Transparency Log ──────────────────────────
	TransparencyEnabled      bool          `env:"TRANSPARENCY_LOG_ENABLED, default=false"`
	TransparencyWebhookURL   string        `env:"TRANSPARENCY_LOG_WEBHOOK_URL"`
	TransparencyPollInterval time.Duration `env:"TRANSPARENCY_LOG_POLL_INTERVAL, default=5m"`
}

// PostgresConfig holds PostgreSQL connection configuration.
type PostgresConfig struct {
	// DSN, if set, is used directly - all other fields are ignored.
	// Use this for managed databases (Neon, RDS, etc.) that provide a
	// full connection string with extra params like channel_binding.
	// Env: FACT0_POSTGRES_DSN
	DSNOverride string `env:"DSN"`

	// ReadDSN, when set, is used for read-only query paths (list, verify,
	// dashboard). Falls back to the primary DSN when empty.
	ReadDSN string `env:"READ_DSN"`

	Host     string `env:"HOST, default=localhost"`
	Port     int    `env:"PORT, default=5432"`
	User     string `env:"USER, default=fact0"`
	Password string `env:"PASSWORD"`
	Database string `env:"DATABASE, default=fact0"`
	SSLMode  string `env:"SSLMODE, default=disable"`

	// MaxConns is the default total connection budget when write/read
	// overrides are not set (default 25).
	MaxConns int `env:"MAX_CONNS, default=25"`
	MinConns int `env:"MIN_CONNS, default=1"`

	// WriteMaxConns / ReadMaxConns optionally override the split; when
	// zero, defaults to 70% / 30% of MaxConns respectively.
	WriteMaxConns int `env:"WRITE_MAX_CONNS"`
	ReadMaxConns  int `env:"READ_MAX_CONNS"`
}

// DSN returns the PostgreSQL connection string.
// If FACT0_POSTGRES_DSN is set it is returned verbatim; otherwise
// a DSN is built from the individual HOST/PORT/USER/PASSWORD/DATABASE/SSLMODE
// fields (suitable for local contributor dev).
func (c PostgresConfig) DSN() string {
	if c.DSNOverride != "" {
		return c.DSNOverride
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.Database, c.SSLMode,
	)
}

// ReadDSNOrPrimary returns the read-replica DSN or the primary DSN.
func (c PostgresConfig) ReadDSNOrPrimary() string {
	if c.ReadDSN != "" {
		return c.ReadDSN
	}
	return c.DSN()
}

// PoolLimits returns write and read pool max connection counts.
func (c PostgresConfig) PoolLimits() (writeMax, readMax int) {
	total := c.MaxConns
	if total <= 0 {
		total = 25
	}
	writeMax = c.WriteMaxConns
	readMax = c.ReadMaxConns
	if writeMax <= 0 {
		writeMax = total * 70 / 100
		if writeMax < 1 {
			writeMax = 1
		}
	}
	if readMax <= 0 {
		readMax = total - writeMax
		if readMax < 1 {
			readMax = 1
		}
	}
	return writeMax, readMax
}

// Load reads configuration from environment variables.
// Legacy LEDGERFLOW_* variables are copied to FACT0_* when the latter are unset.
func Load(ctx context.Context) (*Config, error) {
	applyLegacyEnvAliases()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var cfg Config
	if err := envconfig.Process(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if cfg.Postgres.DSNOverride == "" && cfg.Postgres.Password == "" {
		return nil, fmt.Errorf("set FACT0_POSTGRES_DSN or FACT0_POSTGRES_PASSWORD; no database password is supplied by default")
	}
	if cfg.Server.OSSMode {
		cfg.Server.RetentionDisabled = true
		cfg.Server.DigestDisabled = true
		cfg.Server.IngestWorkerEnabled = false
		cfg.Server.IngestAsyncDefault = false
		cfg.Server.OTLPHTTPEnabled = false
		cfg.Server.OTLPGRPCEnabled = false
		cfg.Server.TransparencyEnabled = false
		cfg.Server.RateLimitDisabled = true
		cfg.Server.EmailBridgeURL = ""
		cfg.Server.EmailWebhookSecret = ""
		cfg.Server.RazorpayKeyID = ""
		cfg.Server.RazorpayKeySecret = ""
		cfg.Server.RazorpayWebhookSecret = ""
		cfg.Server.MaxBatchBodyBytes = 4 << 20
	}
	return &cfg, nil
}

func applyLegacyEnvAliases() {
	aliasEnvPrefix("LEDGERFLOW_POSTGRES_", "FACT0_POSTGRES_")
	aliasEnvPrefix("LEDGERFLOW_", "FACT0_")
	aliasEnvPrefix("RAZORPAY_", "FACT0_RAZORPAY_")
}

func aliasEnvPrefix(legacyPrefix, targetPrefix string) {
	for _, kv := range os.Environ() {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(key, legacyPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(key, legacyPrefix)
		target := targetPrefix + suffix
		if os.Getenv(target) == "" {
			_ = os.Setenv(target, val)
		}
	}
}
