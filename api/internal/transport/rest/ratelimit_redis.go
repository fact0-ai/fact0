package rest

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// RedisRateLimitBackend implements distributed event-weighted rate limiting.
type RedisRateLimitBackend struct {
	client *redis.Client
	logger zerolog.Logger
}

// NewRedisRateLimitBackend dials Redis and returns a RateLimitBackend.
func NewRedisRateLimitBackend(url string, logger zerolog.Logger) (*RedisRateLimitBackend, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &RedisRateLimitBackend{
		client: client,
		logger: logger.With().Str("component", "ratelimit_redis").Logger(),
	}, nil
}

// AllowN implements a fixed-window counter keyed by identifier + kind.
func (b *RedisRateLimitBackend) AllowN(ctx context.Context, key, kind string, r rate.Limit, burst, n int) (bool, time.Duration) {
	if n <= 0 {
		n = 1
	}
	window := time.Second
	limit := int(r)
	if limit < 1 {
		limit = 1
	}
	if burst > limit {
		limit = burst
	}

	now := time.Now()
	windowID := now.Unix()
	redisKey := fmt.Sprintf("rl:%s:%s:%d", kind, key, windowID)

	pipe := b.client.Pipeline()
	incr := pipe.IncrBy(ctx, redisKey, int64(n))
	pipe.Expire(ctx, redisKey, window*2)
	_, err := pipe.Exec(ctx)
	if err != nil {
		b.logger.Warn().Err(err).Msg("redis rate limit error - allowing request")
		return true, 0
	}

	count, err := incr.Result()
	if err != nil {
		return true, 0
	}
	if count > int64(limit) {
		retry := time.Until(now.Truncate(window).Add(window))
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry
	}
	return true, 0
}

// Close closes the Redis client.
func (b *RedisRateLimitBackend) Close() error {
	return b.client.Close()
}
