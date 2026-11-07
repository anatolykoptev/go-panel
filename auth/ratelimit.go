package auth

import (
	"context"
	"time"
)

// RateLimiter throttles the LoginHandler POST branch by key (see
// BcryptConfig.ClientIP). auth deliberately declares this LOCAL interface
// instead of importing identity.RateLimiter — identity root -> identity/session
// -> go-redis, which would drag go-redis into every bcrypt-only consumer (the
// same dependency-light rationale as auth's local Observer/Op/Outcome seam,
// see observer.go). The signature is identical to identity.RateLimiter BY
// DESIGN (identity/ratelimit.go) so go-grad's EXISTING identity Redis limiter
// satisfies this interface structurally — zero import, zero second limiter.
type RateLimiter interface {
	// Allow reports whether the event under key is permitted now. A false
	// return means the caller is over the limit. A non-nil error is an
	// infrastructure failure — checkRateLimit treats both as fail-closed
	// (deny the login attempt), matching identity's tested convention
	// (identity/handlers.go's allowStart + identity/ratelimit/redis_test.go
	// TestRedisLimiter_FailsClosedOnRedisError): the money-path admin login
	// must fail at least as safe as magic-link.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// RateRule is a (limit, window) pair for the login throttle
// (BcryptConfig.LoginRate).
type RateRule struct {
	Limit  int
	Window time.Duration
}

// BlockedChecker is the optional RateLimiter capability to ask "is this key over
// its limit?" without counting a hit. A caller that wants to count only some
// events (the settings page counts failed current-password checks, not
// successes or CSRF failures) uses it before the work and Allow only for the
// failures. MemoryLimiter implements it; a limiter that does not (a shared
// Redis one) gets the conservative fallback of counting every attempt.
type BlockedChecker interface {
	Blocked(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}
