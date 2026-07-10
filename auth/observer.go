package auth

import "time"

// Op identifies which auth-package handler produced an observation.
//
// auth deliberately does NOT import identity (identity root -> identity/session
// -> go-redis, which would drag go-redis into every bcrypt-only consumer), so
// this is a local, dependency-light enum rather than a re-export of
// identity.Op. A single identity/promobs.Observer adapts both seams onto the
// same Prometheus metric family — see promobs.Observer.AsAuthObserver.
type Op uint8

const (
	// OpSessionRecheck is the per-request liveSession revocation recheck
	// (Require -> liveSession -> AccountStore.GetByID).
	OpSessionRecheck Op = iota + 1
	// OpBcryptLogin is the LoginHandler POST branch (email + password check).
	// Wired in a later phase; the value is reserved now so the enum does not
	// churn across phases.
	OpBcryptLogin
)

// Outcome classifies the terminal result of an auth-package operation.
type Outcome uint8

const (
	// OutcomeOK means the operation completed successfully.
	OutcomeOK Outcome = iota + 1
	// OutcomeInvalidCredentials means a login attempt failed (unknown email,
	// wrong password) or a session failed its live revocation check.
	OutcomeInvalidCredentials
	// OutcomeError means an infrastructure failure occurred (e.g. the
	// AccountStore returned a transient, non-not-found error).
	OutcomeError
	// OutcomeRateLimited means the attempt was rejected by the rate limiter
	// as over-quota. Reserved for the RateLimiter hook phase.
	OutcomeRateLimited
	// OutcomeLimiterError means the RateLimiter itself failed (e.g. a Redis
	// outage) — distinct from OutcomeRateLimited, which means the limiter
	// answered and denied the attempt. Reserved for the RateLimiter hook phase.
	OutcomeLimiterError
)

// Observer receives a single observation per completed auth-package
// operation. Implementations must be concurrency-safe — Observe is called
// from arbitrary request-handling goroutines.
//
// Typical use: a host wires identity/promobs.Observer (via
// AsAuthObserver) into BcryptConfig.Observer so one concrete Prometheus
// observer serves both the identity and auth seams.
type Observer interface {
	// Observe is called exactly once per observed operation, after the
	// terminal outcome is known. op and outcome identify the operation and
	// its result; dur is the wall-clock time spent on that operation.
	Observe(op Op, outcome Outcome, dur time.Duration)
}

// NopObserver is a no-op [Observer] used as the default when
// BcryptConfig.Observer is nil. It satisfies the interface without allocating.
type NopObserver struct{}

// Observe discards all observations.
func (NopObserver) Observe(Op, Outcome, time.Duration) {}
