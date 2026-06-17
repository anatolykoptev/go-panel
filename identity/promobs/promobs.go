// Package promobs provides a Prometheus-backed identity.Observer for the
// go-panel/identity framework. It lives in its own package so a host that does
// not use Prometheus never compiles the client_golang dependency. Wire it into
// identity.Config.Observer.
package promobs

import (
	"time"

	"github.com/anatolykoptev/go-panel/identity"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Stable metric label values (snake_case; never change — they are label values).
const (
	opMagicStart  = "magic_start"
	opMagicVerify = "magic_verify"
	opLogout      = "logout"
	opLinkDevice  = "link_device"

	outcomeOK           = "ok"
	outcomeRateLimited  = "rate_limited"
	outcomeInvalidToken = "invalid_token"
	outcomeBadRequest   = "bad_request"
	outcomeError        = "error"
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
		return "unknown"
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
		return "unknown"
	}
}
