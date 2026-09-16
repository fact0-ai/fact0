// Package main is the entry point for the Fact0 server.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/fact0-ai/fact0/internal/analytics"
	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/copilot"
	"github.com/fact0-ai/fact0/internal/digest"
	"github.com/fact0-ai/fact0/internal/ingest/outbox"
	"github.com/fact0-ai/fact0/internal/ingestion"
	"github.com/fact0-ai/fact0/internal/lineage"
	"github.com/fact0-ai/fact0/internal/notify"
	fact0otel "github.com/fact0-ai/fact0/internal/otel"
	"github.com/fact0-ai/fact0/internal/platform/config"
	"github.com/fact0-ai/fact0/internal/platform/logging"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
	"github.com/fact0-ai/fact0/internal/query"
	"github.com/fact0-ai/fact0/internal/replay"
	"github.com/fact0-ai/fact0/internal/retention"
	"github.com/fact0-ai/fact0/internal/share"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
	"github.com/fact0-ai/fact0/internal/transport/rest"
)

// buildVersion / buildCommit can be set at build time via:
//
//	go build -ldflags "-X main.buildVersion=v0.5.0 -X main.buildCommit=$(git rev-parse --short HEAD)"
//
// They default to "dev"/"unknown" for local builds.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load configuration
	cfg, err := config.Load(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger before anything else so all fatal messages are
	// structured and visible in CloudWatch / whatever log aggregator is
	// attached. "auto" → JSON when not a TTY (ECS, CI), console otherwise.
	logger := logging.NewLogger(cfg.Server.LogLevel, cfg.Server.LogFormat)

	logger.Info().
		Str("log_level", cfg.Server.LogLevel).
		Str("log_format", cfg.Server.LogFormat).
		Int("port", cfg.Server.Port).
		Bool("jwks_configured", cfg.Server.BetterAuthJWKSURL != "").
		Bool("webhook_configured", cfg.Server.AuthWebhookSecret != "").
		Msg("starting Fact0")

	// Connect to PostgreSQL - separate write/read pools when READ_DSN is set.
	writeMax, readMax := cfg.Postgres.PoolLimits()
	poolOpts := postgres.PoolOptions{MinConns: cfg.Postgres.MinConns}
	poolOpts.MaxConns = writeMax
	writePool, err := postgres.ConnectPool(ctx, cfg.Postgres.DSN(), poolOpts)
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("hint", "check FACT0_POSTGRES_DSN env var and that the DB is reachable").
			Msg("failed to connect to postgres write pool")
	}
	defer writePool.Close()

	poolOpts.MaxConns = readMax
	readPool, err := postgres.ConnectPool(ctx, cfg.Postgres.ReadDSNOrPrimary(), poolOpts)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to postgres read pool")
	}
	defer readPool.Close()
	logger.Info().
		Int("write_max_conns", writeMax).
		Int("read_max_conns", readMax).
		Bool("read_replica", cfg.Postgres.ReadDSN != "").
		Msg("connected to postgres")

	go postgres.StartPoolMetrics(ctx, writePool, readPool)

	// Initialize storage
	writeStore := postgres.NewStore(writePool, logger)
	readStore := postgres.NewStore(readPool, logger)
	auditStore := postgres.NewAuditStore(writePool, readPool, logger)
	auditStore.SetOSSMode(cfg.Server.OSSMode)
	copilotStore := postgres.NewCopilotStore(writePool, logger)

	// Initialize services
	ingestionSvc := ingestion.NewService(writeStore, logger, cfg.Server.RedactionEnabled)
	lineageSvc := lineage.NewService(readStore, logger)
	querySvc := query.NewService(readStore, lineageSvc, logger)
	replaySvc := replay.NewService(readStore, logger)

	outboxStore := outbox.NewStore(writePool, logger)
	outboxAdapter := outbox.NewAdapter(outboxStore)
	var signingKey ed25519.PrivateKey
	if cfg.Server.SigningKey != "" {
		if decoded, err := base64.StdEncoding.DecodeString(cfg.Server.SigningKey); err == nil && len(decoded) == ed25519.PrivateKeySize {
			signingKey = ed25519.PrivateKey(decoded)
		} else if decoded, err := hex.DecodeString(cfg.Server.SigningKey); err == nil && len(decoded) == ed25519.PrivateKeySize {
			signingKey = ed25519.PrivateKey(decoded)
		} else {
			logger.Fatal().Msg("FACT0_SIGNING_KEY must be a valid 64-byte base64 or hex encoded ed25519 private key")
		}
	}

	auditOptions := audit.ServiceOptions{
		Enqueuer:     outboxAdapter,
		Receipts:     outboxAdapter,
		AsyncDefault: cfg.Server.IngestAsyncDefault,
		SigningKey:   signingKey,
	}
	if cfg.Server.OSSMode {
		auditOptions.Enqueuer = nil
		auditOptions.Receipts = nil
	}
	auditSvc := audit.NewServiceWithOptions(auditStore, logger, cfg.Server.RedactionEnabled, auditOptions)
	keyStore := auth.NewCachedKeyStore(auditStore, cfg.Server.AuthKeyCacheTTL)
	copilotSvc := copilot.NewService(
		copilotStore,
		&copilotPlanAdapter{store: auditStore},
		cfg.Server.CopilotSessionTTLDays,
		logger,
	)
	logger.Info().Bool("enabled", cfg.Server.RedactionEnabled).Msg("ingest redaction configured")

	// JWKS-backed verifier for dashboard JWTs (Better Auth). nil when
	// FACT0_BETTER_AUTH_JWKS_URL is unset - RequireJWT then 404s
	// every /v1/me/* request, which is the desired behaviour for local
	// dev without the dashboard running.
	var jwtVerifier *auth.Verifier
	if cfg.Server.BetterAuthJWKSURL != "" {
		v, err := auth.NewVerifier(ctx,
			cfg.Server.BetterAuthJWKSURL,
			cfg.Server.BetterAuthIssuer,
			cfg.Server.BetterAuthAuthorizedParties,
			logger,
		)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to init JWT verifier")
		}
		jwtVerifier = v
		logger.Info().Str("jwks_url", cfg.Server.BetterAuthJWKSURL).Msg("JWT verifier ready")
	} else {
		logger.Warn().Msg("FACT0_BETTER_AUTH_JWKS_URL unset - /v1/me/* disabled")
	}

	// HMAC-signed SSE ticket store. Uses AuthWebhookSecret as the
	// signing key so any ECS task can validate tickets issued by a
	// sibling - required when running multiple tasks behind the ALB.
	sseTickets := auth.NewSSETicketStore(cfg.Server.AuthWebhookSecret)

	shareStore := share.NewStore(writePool, logger)
	emailBridge := notify.NewEmailBridge(cfg.Server.EmailBridgeURL, cfg.Server.EmailWebhookSecret)

	adminStore := postgres.NewAdminStore(readPool, logger)
	adminHandler := rest.NewAdminHandler(adminStore, auditSvc, emailBridge, logger)

	// Initialize HTTP handlers
	telemetryHandler := rest.NewHandler(ingestionSvc, querySvc, replaySvc, logger, cfg.Server.CORSAllowedOrigins)
	alerter := notify.NewAlerter(shareStore, emailBridge, logger)
	auditConfig := rest.AuditHandlerConfig{
		OSSMode:           cfg.Server.OSSMode,
		MaxBatchBodyBytes: cfg.Server.MaxBatchBodyBytes,
		AsyncDefault:      cfg.Server.IngestAsyncDefault,
		MonthlyLimiter: audit.NewMonthlyLimiter(
			auditStore,
			auditStore,
			shareStore,
			alerter,
			auditStore,
			cfg.Server.FreeMonthlyEventLimit,
		),
	}
	if cfg.Server.OSSMode {
		auditConfig.MonthlyLimiter = nil
		alerter = nil
	}
	auditHandler := rest.NewAuditHandler(auditSvc, auditStore, alerter, logger, auditConfig)
	meHandler := rest.NewMeHandler(auditStore, auditStore, auditStore, auditStore, auditStore, sseTickets, logger)
	meHandler.SetOSSMode(cfg.Server.OSSMode)
	copilotHandler := rest.NewCopilotHandler(copilotSvc, auditStore, logger)
	shareHandler := rest.NewShareHandler(shareStore, auditStore, alerter, logger)
	analyticsSvc := analytics.NewService(writeStore)
	if !cfg.Server.OSSMode {
		analytics.StartPricingSync(ctx, 24*time.Hour)
	}
	analyticsHandler := rest.NewAnalyticsHandler(auditStore, analyticsSvc, logger)
	ccSessionsHandler := rest.NewClaudeCodeSessionsHandler(auditStore, logger)
	ccPolicyHandler := rest.NewClaudeCodePolicyHandler(auditStore, auditStore, auditSvc, logger)

	// Build info gauge - single-sample, labels are what dashboards key off.
	goVersion := runtime.Version()
	if info, ok := debug.ReadBuildInfo(); ok && buildCommit == "unknown" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				buildCommit = s.Value
				break
			}
		}
	}
	metrics.BuildInfo.WithLabelValues(buildVersion, buildCommit, goVersion).Set(1)
	logger.Info().
		Str("version", buildVersion).
		Str("commit", buildCommit).
		Str("go_version", goVersion).
		Msg("build info")

	router := chi.NewRouter()
	// Metrics middleware first so it wraps everything (auth, rate limit, handler).
	router.Use(rest.MetricsMiddleware)
	telemetryHandler.ApplyBaseMiddleware(router)

	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"fact0"}`))
	})

	// Prometheus scrape endpoint. Intended for the CWAgent sidecar
	// running inside the same ECS task - the ALB does not forward
	// requests to this path (see scripts/aws/migrate-region.sh listener
	// rules). Authentication intentionally omitted because the endpoint
	// is only reachable on the task-local loopback / ECS task ENI.
	router.Method(http.MethodGet, "/metrics", promhttp.Handler())

	// Ingest rate limiter - single instance shared across all ingest
	// routes so the same caller has one bucket no matter which endpoint
	// it hits. Pass nil to MountTelemetry / use identityMW to disable.
	var rateLimiter *rest.RateLimiter
	var redisRL *rest.RedisRateLimitBackend
	if !cfg.Server.RateLimitDisabled {
		rlCfg := rest.RateLimitConfig{
			EventsPerSec:     cfg.Server.RateLimitEventsPerSec,
			EventsBurst:      cfg.Server.RateLimitEventsBurst,
			ExecutionsPerMin: cfg.Server.RateLimitExecutionsPerMin,
		}
		if cfg.Server.RateLimitBackend == "redis" && cfg.Server.RedisURL != "" {
			var err error
			redisRL, err = rest.NewRedisRateLimitBackend(cfg.Server.RedisURL, logger)
			if err != nil {
				logger.Warn().Err(err).Msg("redis rate limit unavailable - falling back to in-memory")
				rateLimiter = rest.NewRateLimiter(rlCfg, logger)
			} else {
				rateLimiter = rest.NewRateLimiterWithBackend(rlCfg, logger, redisRL)
				logger.Info().Str("redis_url", cfg.Server.RedisURL).Msg("redis rate limiter enabled")
			}
		} else {
			rateLimiter = rest.NewRateLimiter(rlCfg, logger)
		}
		defer rateLimiter.Stop()
		if redisRL != nil {
			defer func() { _ = redisRL.Close() }()
		}
		logger.Info().
			Int("events_per_sec", cfg.Server.RateLimitEventsPerSec).
			Int("events_burst", cfg.Server.RateLimitEventsBurst).
			Int("executions_per_min", cfg.Server.RateLimitExecutionsPerMin).
			Str("backend", cfg.Server.RateLimitBackend).
			Msg("rate limiter enabled")
	} else {
		logger.Warn().Msg("rate limiter disabled via FACT0_RATELIMIT_DISABLED")
	}

	// Telemetry routes - API-key ingest, JWT/API-key reads (tenant-scoped).
	telemetryHandler.MountTelemetry(router, rateLimiter, &rest.TelemetryAuth{
		KeyStore:    keyStore,
		Tenants:     auditStore,
		JWTVerifier: jwtVerifier,
	})

	// OTLP/HTTP receiver - OpenTelemetry native ingestion.
	// Accepts ExportTraceServiceRequest at POST /v1/otlp/v1/traces.
	// Disabled by default; enable via FACT0_OTLP_HTTP_ENABLED=true.
	if cfg.Server.OTLPHTTPEnabled {
		otlpHandler := fact0otel.NewHandler(ingestionSvc, logger)
		rest.MountOTLP(router, otlpHandler.ExportTraces, &rest.TelemetryAuth{
			KeyStore:    keyStore,
			Tenants:     auditStore,
			JWTVerifier: jwtVerifier,
		}, rateLimiter)
		logger.Info().Msg("OTLP/HTTP receiver mounted at /v1/otlp/v1/traces")
	} else {
		logger.Info().Msg("OTLP/HTTP receiver disabled (set FACT0_OTLP_HTTP_ENABLED=true to enable)")
	}

	// OTLP/gRPC receiver - separate listener on :4317 (IANA-registered).
	// This is the default transport for most OTel SDKs and collectors.
	// Disabled by default; enable via FACT0_OTLP_GRPC_ENABLED=true.
	var otlpGRPC *fact0otel.GRPCServer
	if cfg.Server.OTLPGRPCEnabled {
		otlpGRPC = fact0otel.NewGRPCServer(ingestionSvc, keyStore, logger)
		go func() {
			logger.Info().Int("port", cfg.Server.OTLPGRPCPort).
				Msg("OTLP/gRPC receiver listening")
			if err := otlpGRPC.ListenAndServe(cfg.Server.OTLPGRPCPort); err != nil {
				logger.Fatal().Err(err).Msg("OTLP/gRPC server failed")
			}
		}()
		logger.Info().Int("port", cfg.Server.OTLPGRPCPort).Msg("OTLP/gRPC receiver enabled")
	} else {
		logger.Info().Msg("OTLP/gRPC receiver disabled (set FACT0_OTLP_GRPC_ENABLED=true to enable)")
	}

	// Audit ingestion - bare API key + write scope. Never reachable
	// from the dashboard, so a compromised browser session cannot
	// forge audit events. Rate-limited by api_key_id when enabled.
	router.Group(func(r chi.Router) {
		r.Use(auth.APIKey(keyStore))
		if rateLimiter != nil {
			r.Use(rateLimiter.Events())
		}
		auditHandler.MountWrite(r)
		// Agentless Claude Code hook ingest - same auth + rate-limit chain,
		// write scope enforced internally (mirrors MountWrite).
		auditHandler.MountIntegrations(r)
	})

	// Audit reads - DualAuth accepts either an API key (SDK path) or
	// a verified dashboard JWT, and projects the latter to a read-scope
	// context so the dashboard never needs to hold a raw alk_live_* key.
	router.Group(func(r chi.Router) {
		r.Use(auth.DualAuth(keyStore, auditStore, jwtVerifier))
		auditHandler.MountRead(r)
		// Aggregated Claude Code sessions for the dashboard Coding Agents view.
		ccSessionsHandler.Mount(r)
		// Collector-facing governance policy fetch.
		if !cfg.Server.OSSMode {
			ccPolicyHandler.MountRead(r)
		}
	})

	// Audit SSE stream - single-use ticket (browser) or bearer API key
	// (SDK). No credentials in URLs.
	router.Group(func(r chi.Router) {
		r.Use(auth.SSEAuth(sseTickets, keyStore))
		auditHandler.MountStream(r)
	})

	// Public share link metadata (no event content).
	if !cfg.Server.OSSMode {
		router.Get("/v1/public/share-links/{id}/meta", shareHandler.PublicMeta)

		// Share-link authenticated read-only audit surface.
		router.Group(func(r chi.Router) {
			r.Use(auth.ShareAuth(shareStore))
			auditHandler.MountShare(r)
		})

	}

	// Dashboard routes - gated by dashboard JWT (org context required).
	router.Group(func(r chi.Router) {
		r.Use(auth.RequireJWT(jwtVerifier))
		r.Use(auth.RequireOrg)
		meHandler.Mount(r)
		if !cfg.Server.OSSMode {
			shareHandler.Mount(r)
			copilotHandler.Mount(r)
		}
		analyticsHandler.Mount(r)
		// Governance policy editor for the Coding Agents view.
		if !cfg.Server.OSSMode {
			ccPolicyHandler.MountDashboard(r)
		}
	})

	if !cfg.Server.OSSMode {
		// Platform admin - cross-tenant; gated by JWT platformRole claim.
		router.Group(func(r chi.Router) {
			r.Use(auth.RequireJWT(jwtVerifier))
			adminHandler.Mount(r)
		})

	}

	if !cfg.Server.OSSMode {
		// Auth webhooks - HMAC-signed bridge from Better Auth running in
		// the Next.js process. Disabled (route returns 404) when the secret
		// is empty.
		if webhookHandler := rest.NewAuthWebhookHandler(
			auditStore, auditStore, auditStore,
			cfg.Server.AuthWebhookSecret,
			logger,
		); webhookHandler != nil {
			webhookHandler.Mount(router)
			logger.Info().Msg("auth webhook handler mounted at /webhooks/auth")
		} else {
			logger.Warn().Msg("FACT0_AUTH_WEBHOOK_SECRET unset - /webhooks/auth disabled")
		}

	}
	if !cfg.Server.OSSMode {
		// Razorpay webhooks - processes Razorpay checkout completions and payment status receipts.
		razorpayWebhookSecret := strings.Trim(cfg.Server.RazorpayWebhookSecret, "\"' ")
		if razorpayWebhookSecret == "" {
			razorpayWebhookSecret = strings.Trim(cfg.Server.RazorpayKeySecret, "\"' ")
		}
		razorpayWebhookHandler := rest.NewRazorpayWebhookHandler(
			auditStore,
			auditSvc,
			razorpayWebhookSecret,
			logger,
		)
		razorpayWebhookHandler.Mount(router)
		logger.Info().Msg("razorpay webhook handler mounted at /api/webhooks/razorpay")

	}

	// Retention sweeper - runs as a goroutine, stops cleanly when ctx
	// is cancelled at shutdown. Skipped if explicitly disabled.
	if !cfg.Server.RetentionDisabled {
		sweeper := retention.NewSweeper(writePool, retention.Config{
			Interval:              cfg.Server.RetentionInterval,
			BatchSize:             cfg.Server.RetentionBatchSize,
			DefaultRetentionDays:  cfg.Server.RetentionFallbackDays,
			CopilotSessionTTLDays: cfg.Server.CopilotSessionTTLDays,
		}, emailBridge, logger)
		go sweeper.Start(ctx)

		// Weekly coding-agents digest rides the same lifecycle.
		if !cfg.Server.DigestDisabled {
			dw := digest.NewWorker(writePool, auditStore, emailBridge, digest.Config{
				Interval: cfg.Server.DigestInterval,
			}, logger)
			go dw.Start(ctx)
		} else {
			logger.Warn().Msg("coding agents digest disabled via FACT0_DIGEST_DISABLED")
		}
	} else {
		logger.Warn().Msg("retention sweeper disabled via FACT0_RETENTION_DISABLED")
	}

	if cfg.Server.IngestWorkerEnabled {
		worker := outbox.NewWorker(outboxStore, outbox.ServiceCommitter{Svc: auditSvc}, alerter, logger, outbox.WorkerConfig{
			BatchSize:   cfg.Server.IngestWorkerBatchSize,
			MaxAttempts: cfg.Server.IngestWorkerMaxAttempts,
		})
		go worker.Run(ctx, cfg.Server.IngestWorkerPollInterval)
		logger.Info().
			Dur("poll_interval", cfg.Server.IngestWorkerPollInterval).
			Int("batch_size", cfg.Server.IngestWorkerBatchSize).
			Msg("ingest outbox worker started")
	} else {
		logger.Warn().Msg("ingest outbox worker disabled via FACT0_INGEST_WORKER_ENABLED=false")
	}

	if cfg.Server.TransparencyEnabled {
		transparencyWorker := audit.NewTransparencyWorker(readPool, auditStore, audit.TransparencyConfig{
			Enabled:      cfg.Server.TransparencyEnabled,
			WebhookURL:   cfg.Server.TransparencyWebhookURL,
			PollInterval: cfg.Server.TransparencyPollInterval,
		}, logger)
		go transparencyWorker.Start(ctx)
	} else {
		logger.Warn().Msg("transparency log worker disabled")
	}

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info().Str("addr", addr).Msg("HTTP server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	<-done
	logger.Info().Msg("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("shutdown error")
	}

	// Stop OTLP/gRPC server if running.
	if otlpGRPC != nil {
		otlpGRPC.GracefulStop()
		logger.Info().Msg("OTLP/gRPC server stopped")
	}

	logger.Info().Msg("server stopped")
}

// copilotPlanAdapter bridges audit.PlanStore → copilot.PlanStore so the
// copilot package stays decoupled from the audit domain.
type copilotPlanAdapter struct {
	store audit.PlanStore
}

func (a *copilotPlanAdapter) GetTenantPlan(ctx context.Context, tenantID string) (*copilot.Plan, error) {
	p, err := a.store.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &copilot.Plan{
		ID:                  p.ID,
		MonthlyCopilotLimit: p.MonthlyCopilotLimit,
	}, nil
}
