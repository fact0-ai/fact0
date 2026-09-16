package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/fact0-ai/fact0/internal/audit"
	"github.com/fact0-ai/fact0/internal/auth"
	"github.com/fact0-ai/fact0/internal/platform/metrics"
)

// RateLimitConfig defines the two ingest buckets we rate-limit.
//
// Two separate limiters exist because the workloads have very different
// shapes: one execution is created per agent run, but each run can emit
// hundreds of spans/events. A single bucket sized for executions would
// throttle span ingest; sized for spans, it would let a runaway client
// open thousands of executions.
type RateLimitConfig struct {
	// EventsPerSec is the steady-state token rate for span/event ingest.
	EventsPerSec int
	// EventsBurst is the maximum burst size for the events bucket.
	EventsBurst int
	// ExecutionsPerMin is the steady-state rate for new executions.
	ExecutionsPerMin int
}

// DefaultRateLimitConfig matches the values documented in the plan.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		EventsPerSec:     50,
		EventsBurst:      200,
		ExecutionsPerMin: 120,
	}
}

// rateLimitBucket pairs a token bucket with a touch timestamp so the
// sweeper can evict idle entries without locking the whole map.
type rateLimitBucket struct {
	limiter *rate.Limiter
	touched int64 // unix-nano, updated on every Allow()
}

// RateLimiter holds the per-identifier token buckets for ingest routes.
// One instance is shared across all middleware constructors so the same
// caller has one bucket regardless of which ingest route it hits within
// the same "kind".
//
// In-memory by design: the API runs as a single ECS task today and a
// distributed limiter (Redis, DynamoDB) would add latency on the hot
// path with no benefit. If we ever horizontally scale, replace the
// internal sync.Maps with a Redis-backed implementation behind the
// same Middleware interface - the call sites won't change.
type RateLimiter struct {
	cfg     RateLimitConfig
	logger  zerolog.Logger
	backend RateLimitBackend
	events  sync.Map // identifier → *rateLimitBucket
	execs   sync.Map // identifier → *rateLimitBucket
	stop    chan struct{}
	stopped sync.Once
}

// RateLimitBackend allows distributed rate limiting. When nil, in-memory
// token buckets are used.
type RateLimitBackend interface {
	AllowN(ctx context.Context, key, kind string, r rate.Limit, burst, n int) (allowed bool, retryAfter time.Duration)
}

// NewRateLimiter constructs a limiter and starts the idle-bucket sweeper.
func NewRateLimiter(cfg RateLimitConfig, logger zerolog.Logger) *RateLimiter {
	return NewRateLimiterWithBackend(cfg, logger, nil)
}

// NewRateLimiterWithBackend constructs a limiter with an optional distributed backend.
func NewRateLimiterWithBackend(cfg RateLimitConfig, logger zerolog.Logger, backend RateLimitBackend) *RateLimiter {
	rl := &RateLimiter{
		cfg:     cfg,
		logger:  logger.With().Str("component", "ratelimit").Logger(),
		backend: backend,
		stop:    make(chan struct{}),
	}
	go rl.sweep()
	return rl
}

// Stop terminates the background sweeper. Safe to call multiple times.
func (rl *RateLimiter) Stop() {
	rl.stopped.Do(func() { close(rl.stop) })
}

// idleBucketTTL is how long an unused bucket lives before the sweeper
// evicts it. Picked to be longer than the worst plausible quiet period
// for an active client (5 min) so we don't reset their state mid-session.
const idleBucketTTL = 10 * time.Minute

func (rl *RateLimiter) sweep() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case now := <-t.C:
			cut := now.Add(-idleBucketTTL).UnixNano()
			evict := func(m *sync.Map) {
				m.Range(func(k, v any) bool {
					b, _ := v.(*rateLimitBucket)
					if b != nil && b.touched < cut {
						m.Delete(k)
					}
					return true
				})
			}
			evict(&rl.events)
			evict(&rl.execs)
		}
	}
}

// Events returns chi middleware that limits span/event ingest. Mount on
// routes that accept high-frequency child data (spans, events,
// end-of-execution).
func (rl *RateLimiter) Events() func(http.Handler) http.Handler {
	return rl.middleware(&rl.events, rate.Limit(rl.cfg.EventsPerSec), rl.cfg.EventsBurst, "events")
}

// Executions returns chi middleware that limits new-execution creation.
// Mount on POST /api/v1/executions only.
func (rl *RateLimiter) Executions() func(http.Handler) http.Handler {
	// rate.Limit is tokens/sec; we want N/min so divide by 60.
	r := rate.Limit(float64(rl.cfg.ExecutionsPerMin) / 60.0)
	// Burst = full minute's worth, so a client that's been idle can
	// open up to ExecutionsPerMin runs at once before getting paced.
	return rl.middleware(&rl.execs, r, rl.cfg.ExecutionsPerMin, "executions")
}

func (rl *RateLimiter) middleware(
	store *sync.Map,
	r rate.Limit,
	burst int,
	kind string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			id, bucketLabel := identify(req)
			cost := eventCostForRequest(req)
			if cost < 1 {
				cost = 1
			}
			if cost > audit.MaxBatchSize {
				cost = audit.MaxBatchSize
			}

			if rl.backend != nil {
				allowed, retryAfter := rl.backend.AllowN(req.Context(), id, kind, r, burst, cost)
				if !allowed {
					secs := retryAfterSeconds(retryAfter)
					metrics.RateLimitDropped.WithLabelValues(routeLabel(req), kind, bucketLabel).Inc()
					writeRateLimited(w, secs)
					return
				}
				next.ServeHTTP(w, req)
				return
			}

			b := getOrCreateBucket(store, id, r, burst)
			b.touched = time.Now().UnixNano()

			if !b.limiter.AllowN(time.Now(), cost) {
				reservation := b.limiter.ReserveN(time.Now(), cost)
				delay := reservation.Delay()
				reservation.Cancel()
				secs := retryAfterSeconds(delay)
				metrics.RateLimitDropped.WithLabelValues(routeLabel(req), kind, bucketLabel).Inc()
				rl.logger.Debug().
					Str("identifier", id).
					Str("kind", kind).
					Int("cost", cost).
					Int("retry_after_seconds", secs).
					Msg("rate limit exceeded")
				writeRateLimited(w, secs)
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}

func getOrCreateBucket(store *sync.Map, id string, r rate.Limit, burst int) *rateLimitBucket {
	if v, ok := store.Load(id); ok {
		return v.(*rateLimitBucket)
	}
	nb := &rateLimitBucket{limiter: rate.NewLimiter(r, burst)}
	actual, loaded := store.LoadOrStore(id, nb)
	if loaded {
		return actual.(*rateLimitBucket)
	}
	return nb
}

// identify resolves the per-request bucket key. Authenticated callers
// (audit ingest) are keyed by api_key_id so they can't be punished for
// noisy neighbours on the same NAT. Anonymous telemetry callers fall
// back to RealIP. The bucket label is exported as a Prometheus label so
// dashboards can distinguish authenticated vs anonymous drops.
func identify(r *http.Request) (id string, bucketLabel string) {
	if k := auth.KeyIDFromContext(r.Context()); k != "" {
		return "key:" + k, "api_key"
	}
	ip := r.RemoteAddr
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		// First IP in the list is the client; trim any port from RealIP.
		// middleware.RealIP normalises this for r.RemoteAddr already, so
		// X-Forwarded-For is just a safety net for environments without it.
		if comma := indexByte(h, ','); comma > 0 {
			ip = h[:comma]
		} else {
			ip = h
		}
	}
	return "ip:" + ip, "ip"
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// routeLabel keeps Prometheus cardinality bounded by mapping dynamic
// chi path params back to their template form (`{executionID}`).
func routeLabel(r *http.Request) string {
	// chi exposes the matched route template via RouteContext, but
	// resolving it here would require chi.RouteContext(r.Context()).
	// For our small ingest surface we can hard-map by Method+prefix
	// to avoid both the import dependency and the per-request alloc.
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && p == "/api/v1/executions":
		return "POST /api/v1/executions"
	case r.Method == http.MethodPost && hasSuffix(p, "/spans"):
		return "POST /api/v1/executions/{id}/spans"
	case r.Method == http.MethodPost && hasSuffix(p, "/events"):
		return "POST /api/v1/executions/{id}/events"
	case r.Method == http.MethodPut && hasSuffix(p, "/end"):
		return "PUT /api/v1/executions/{id}/end"
	case r.Method == http.MethodPost && p == "/v1/events":
		return "POST /v1/events"
	case r.Method == http.MethodPost && p == "/v1/events/batch":
		return "POST /v1/events/batch"
	}
	return fmt.Sprintf("%s %s", r.Method, p)
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

func writeRateLimited(w http.ResponseWriter, retryAfterSeconds int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":               "rate_limited",
		"retry_after_seconds": retryAfterSeconds,
	})
}

func retryAfterSeconds(delay time.Duration) int {
	if delay <= 0 {
		delay = time.Second
	}
	secs := int(math.Ceil(delay.Seconds()))
	if secs < 1 {
		return 1
	}
	return secs
}

// eventCostForRequest returns how many rate-limit tokens an ingest request consumes.
func eventCostForRequest(r *http.Request) int {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/events":
		return 1
	case r.Method == http.MethodPost && r.URL.Path == "/v1/events/batch":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return 1
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var peek struct {
			Events []json.RawMessage `json:"events"`
		}
		if err := json.Unmarshal(body, &peek); err != nil || len(peek.Events) == 0 {
			return 1
		}
		return len(peek.Events)
	default:
		return 1
	}
}
