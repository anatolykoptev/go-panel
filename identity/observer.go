package identity

import "time"

// Op identifies which auth handler produced an observation.
type Op uint8

const (
	// OpMagicStart is the passwordless-flow initiation handler.
	OpMagicStart Op = iota + 1
	// OpMagicVerify is the token-consumption handler that mints a session.
	OpMagicVerify
	// OpLogout is the session-revocation handler.
	OpLogout
	// OpLinkDevice is the authenticated device-association handler.
	OpLinkDevice
)

// Outcome classifies the terminal result of an auth operation.
type Outcome uint8

const (
	// OutcomeOK means the operation completed successfully.
	OutcomeOK Outcome = iota + 1
	// OutcomeRateLimited means the attempt was rejected by the rate limiter.
	OutcomeRateLimited
	// OutcomeInvalidToken means the magic-link token was absent, expired, or forged.
	OutcomeInvalidToken
	// OutcomeBadRequest means the caller supplied a malformed request body or
	// parameters (invalid email, missing epid, JSON decode error, …).
	OutcomeBadRequest
	// OutcomeError means an infrastructure failure occurred (store, session, email).
	OutcomeError
	// OutcomeLimiterError means the RateLimiter itself failed (e.g. Redis
	// outage) — distinct from OutcomeRateLimited, which means the limiter
	// answered and denied the attempt as over-quota. allowStart fails closed
	// in both cases (denies the attempt either way), but conflating the two
	// outcomes would make an infrastructure outage read as normal throttling.
	OutcomeLimiterError
)

// Observer receives a single observation per completed auth operation.
// Implementations are expected to be concurrency-safe.
//
// Typical use: a go-grad concrete type increments a Prometheus counter keyed by
// (op, outcome) and records dur in a histogram. The framework itself carries no
// metrics import so the host binary controls the metrics library.
type Observer interface {
	// Observe is called exactly once per request, after the terminal outcome is
	// known. op and outcome identify the operation and its result; dur is the
	// wall-clock time from handler entry to the terminal branch.
	Observe(op Op, outcome Outcome, dur time.Duration)
}

// NopObserver is a no-op [Observer] used as the default when Config.Observer is
// nil. It satisfies the interface without allocating.
type NopObserver struct{}

// Observe discards all observations.
func (NopObserver) Observe(Op, Outcome, time.Duration) {}
