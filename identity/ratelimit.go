package identity

import (
	"context"
	"time"
)

// RateLimiter throttles an action identified by key to limit events per window.
// The concrete implementation (e.g. a Redis sliding window) is wired by go-grad;
// the framework only calls Allow.
type RateLimiter interface {
	// Allow reports whether the event under key is permitted now. A false return
	// means the caller is over the limit. A non-nil error is an infrastructure
	// failure (the caller decides fail-open vs fail-closed).
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// RateRule is a (limit, window) pair for a named throttle.
type RateRule struct {
	Limit  int
	Window time.Duration
}
