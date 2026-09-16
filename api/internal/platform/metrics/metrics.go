// Package metrics is the single home for Prometheus collectors used by
// the API. Everything is registered against prometheus.DefaultRegisterer,
// so the /metrics handler can just hand back promhttp.Handler().
//
// Metrics are added phase-by-phase:
//
//   - P1: RateLimitDropped
//   - P3: RetentionDeleted
//   - P5: HTTP request rate/duration/status, ingest accepted/rejected,
//     DB query duration, build info gauge
//
// All collector names use the `fact0_` prefix so a single scrape
// rule covers everything.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RateLimitDropped counts requests rejected by the per-key/IP rate
// limiter. `kind` is "events" or "executions" - matches the limiter
// bucket - and `bucket` is "api_key" or "ip" depending on whether the
// caller was authenticated.
var RateLimitDropped = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fact0_ratelimit_dropped_total",
		Help: "Number of requests rejected by the ingest rate limiter.",
	},
	[]string{"route", "kind", "bucket"},
)

// RetentionDeleted counts rows removed (or tombstoned, for audit_events)
// by the retention sweeper. `table` is the affected table; `tenant_id`
// scopes audit operations and is empty for global telemetry deletes.
var RetentionDeleted = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fact0_retention_deleted_total",
		Help: "Number of rows deleted or tombstoned by the retention sweeper.",
	},
	[]string{"table", "tenant_id"},
)

// RetentionSweepDuration tracks how long each pass of the retention
// sweeper takes. Useful for spotting growing backlogs.
var RetentionSweepDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "fact0_retention_sweep_duration_seconds",
		Help:    "Wall-clock duration of one full retention sweep, by outcome.",
		Buckets: prometheus.ExponentialBuckets(0.1, 4, 8), // 0.1s .. ~1638s
	},
	[]string{"outcome"},
)

// ─── HTTP request metrics (P5) ──────────────────────────────
//
// Route labels are the chi route template (e.g. "/api/v1/executions/{id}")
// rather than the raw path so cardinality stays bounded. The middleware
// in api/internal/transport/rest/metrics_mw.go derives the template from
// chi.RouteContext.

// HTTPRequestsTotal counts every request that reaches the API handler.
var HTTPRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fact0_http_requests_total",
		Help: "Number of HTTP requests served, labelled by route, method, and status.",
	},
	[]string{"route", "method", "status"},
)

// HTTPRequestDuration is a histogram of request latency in seconds.
// Buckets are sized for an API that's mostly small JSON ingest +
// queries: ~1ms up to ~10s.
var HTTPRequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "fact0_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 14), // 1ms .. ~16s
	},
	[]string{"route", "method"},
)

// IngestAccepted counts successful ingest writes. `kind` is one of
// "execution", "span", "event", "audit_event".
var IngestAccepted = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fact0_ingest_accepted_total",
		Help: "Number of ingest entities written successfully.",
	},
	[]string{"kind"},
)

// IngestRejected counts ingest entities the API refused. `reason`
// captures the validation / rate-limit / auth class so dashboards can
// triage failure modes.
var IngestRejected = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "fact0_ingest_rejected_total",
		Help: "Number of ingest entities rejected, labelled by reason.",
	},
	[]string{"kind", "reason"},
)

// BuildInfo is a single-sample gauge that exposes static deployment
// metadata via labels. Always emits value=1 so the labels themselves
// are what dashboards key off.
var BuildInfo = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "fact0_build_info",
		Help: "Static build / runtime info; value is always 1.",
	},
	[]string{"version", "commit", "go_version"},
)

// DBPoolConnections tracks pgx pool connection counts by role (write/read).
var DBPoolConnections = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "fact0_db_pool_connections",
		Help: "Current pgx pool connection counts.",
	},
	[]string{"pool", "stat"},
)

// AuditBatchEvents is the number of events per audit batch ingest.
var AuditBatchEvents = promauto.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "fact0_audit_batch_events",
		Help:    "Audit events per batch ingest request.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 12),
	},
)

// AuditChainCommitDuration tracks AppendBatchChained latency.
var AuditChainCommitDuration = promauto.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "fact0_audit_chain_commit_duration_seconds",
		Help:    "Duration of audit chain batch commits.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 14),
	},
)

// IngestOutboxLagSeconds tracks age of oldest pending outbox row.
var IngestOutboxLagSeconds = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "fact0_ingest_outbox_lag_seconds",
		Help: "Age in seconds of the oldest pending ingest outbox row.",
	},
)
