// Package ratelimit provides a Redis-backed identity.RateLimiter for the
// go-panel/identity framework, so a host need not write its own. It is an
// approximate fixed-window counter (INCR + ExpireNX). Wire it into
// identity.Config.RateLimiter.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter is a fixed-window counter backed by Redis. It satisfies
// github.com/anatolykoptev/go-panel/identity.RateLimiter.
type RedisLimiter struct {
	rdb redis.Cmdable
}

// NewRedisLimiter wraps an existing go-redis client.
func NewRedisLimiter(rdb redis.Cmdable) *RedisLimiter {
	return &RedisLimiter{rdb: rdb}
}

// Allow reports whether the caller identified by key has stayed within limit
// events per window. The window starts on the first hit (ExpireNX) and is never
// extended mid-flight. Fails CLOSED on a Redis error (returns false + error) —
// the framework treats a non-nil error as "deny".
func (r *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	pipe := r.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window) // TTL only on the new key; never extend the window
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("identity/ratelimit: redis: %w", err)
	}
	return incr.Val() <= int64(limit), nil
}
