// Package promobs provides a Prometheus-backed identity.Observer (and, via
// Observer.AsAuthObserver, an auth.Observer) for the go-panel framework. It
// lives in its own package so a host that does not use Prometheus never
// compiles the client_golang dependency. Wire it into identity.Config.Observer
// and/or auth.BcryptConfig.Observer — both seams share the same
// auth_ops_total / auth_op_duration_seconds metric family, distinguished by
// the "op" label.
package promobs

import (
	"time"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/identity"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Stable metric label values (snake_case; never change — they are label values).
const (
	opMagicStart     = "magic_start"
	opMagicVerify    = "magic_verify"
	opLogout         = "logout"
	opLinkDevice     = "link_device"
	opSessionRecheck = "session_recheck"
	opBcryptLogin    = "bcrypt_login"

	outcomeOK           = "ok"
	outcomeRateLimited  = "rate_limited"
	outcomeInvalidToken = "invalid_token"
	outcomeBadRequest   = "bad_request"
	outcomeError        = "error"
	// outcomeInvalidCredentials is a Prometheus metric label value (not a
	// credential) — the linter's secret heuristic matches the const NAME.
	outcomeInvalidCredentials = "invalid_credentials" //nolint:gosec // G101 false positive: Prometheus metric label value, not a credential
	outcomeLimiterError       = "limiter_error"

	// labelUnknown is the fallback label value for an Op/Outcome the mapping
	// functions below don't recognize (e.g. a future enum value observed by
	// an older build). Shared by all four opLabel/outcomeLabel default branches.
	labelUnknown = "unknown"
)

// Observer is the Prometheus-backed identity.Observer. It satisfies
// github.com/anatolykoptev/go-panel/identity.Observer.
type Observer struct {
	ops      *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New registers the auth RED metrics under reg, prefixed by namespace
// (e.g. namespace="go_grad" yields go_grad_auth_ops_total and
// go_grad_auth_op_duration_seconds). Call once during wiring; duplicate
// registration panics (Prometheus behaviour). Pass prometheus.DefaultRegisterer
// for the standard registry.
func New(reg prometheus.Registerer, namespace string) *Observer {
	f := promauto.With(reg)
	return &Observer{
		ops: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "auth_ops_total",
			Help:      "Total auth operations by op and outcome.",
		}, []string{"op", "outcome"}),
		duration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "auth_op_duration_seconds",
			Help:      "Auth operation wall-clock latency by op.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"op"}),
	}
}

// Observe implements identity.Observer.
func (o *Observer) Observe(op identity.Op, outcome identity.Outcome, dur time.Duration) {
	l := opLabel(op)
	o.ops.WithLabelValues(l, outcomeLabel(outcome)).Inc()
	o.duration.WithLabelValues(l).Observe(dur.Seconds())
}

func opLabel(op identity.Op) string {
	switch op {
	case identity.OpMagicStart:
		return opMagicStart
	case identity.OpMagicVerify:
		return opMagicVerify
	case identity.OpLogout:
		return opLogout
	case identity.OpLinkDevice:
		return opLinkDevice
	default:
		return labelUnknown
	}
}

func outcomeLabel(outcome identity.Outcome) string {
	switch outcome {
	case identity.OutcomeOK:
		return outcomeOK
	case identity.OutcomeRateLimited:
		return outcomeRateLimited
	case identity.OutcomeInvalidToken:
		return outcomeInvalidToken
	case identity.OutcomeBadRequest:
		return outcomeBadRequest
	case identity.OutcomeError:
		return outcomeError
	default:
		return labelUnknown
	}
}

// AuthObserver adapts Observer's Prometheus vectors to auth.Observer. It is a
// distinct Go type (not a second method on *Observer) because Go does not
// allow overloading a method name on one receiver type — Observer already
// declares Observe(identity.Op, ...). Both types share the SAME
// *prometheus.CounterVec / *prometheus.HistogramVec, so identity and auth
// observations land in one metric family, distinguished by the "op" label —
// there is still exactly one Prometheus Observer implementation per host,
// exposed as two thin Go views onto it. Obtain one via Observer.AsAuthObserver.
type AuthObserver Observer

// AsAuthObserver returns an *AuthObserver view of o, backed by the same
// Prometheus counters/histogram as o's identity.Observer implementation. Wire
// the result into auth.BcryptConfig.Observer alongside o in
// identity.Config.Observer to share one concrete observer across both seams.
func (o *Observer) AsAuthObserver() *AuthObserver {
	return (*AuthObserver)(o)
}

// Observe implements auth.Observer.
func (o *AuthObserver) Observe(op auth.Op, outcome auth.Outcome, dur time.Duration) {
	l := authOpLabel(op)
	o.ops.WithLabelValues(l, authOutcomeLabel(outcome)).Inc()
	o.duration.WithLabelValues(l).Observe(dur.Seconds())
}

func authOpLabel(op auth.Op) string {
	switch op {
	case auth.OpSessionRecheck:
		return opSessionRecheck
	case auth.OpBcryptLogin:
		return opBcryptLogin
	default:
		return labelUnknown
	}
}

func authOutcomeLabel(outcome auth.Outcome) string {
	switch outcome {
	case auth.OutcomeOK:
		return outcomeOK
	case auth.OutcomeInvalidCredentials:
		return outcomeInvalidCredentials
	case auth.OutcomeError:
		return outcomeError
	case auth.OutcomeRateLimited:
		return outcomeRateLimited
	case auth.OutcomeLimiterError:
		return outcomeLimiterError
	default:
		return labelUnknown
	}
}
