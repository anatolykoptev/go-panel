package promobs_test

import (
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/identity"
	"github.com/anatolykoptev/go-panel/identity/promobs"
	"github.com/prometheus/client_golang/prometheus"
)

// compile-time: Observer satisfies the framework interface.
var _ identity.Observer = (*promobs.Observer)(nil)

func TestObserver_IncrementOnObserve(t *testing.T) {
	reg := prometheus.NewRegistry()
	obs := promobs.New(reg, "test")

	obs.Observe(identity.OpMagicStart, identity.OutcomeOK, 5*time.Millisecond)
	obs.Observe(identity.OpMagicVerify, identity.OutcomeInvalidToken, 10*time.Millisecond)
	obs.Observe(identity.OpLogout, identity.OutcomeError, 1*time.Millisecond)
	obs.Observe(identity.OpLinkDevice, identity.OutcomeBadRequest, 2*time.Millisecond)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	type lp struct{ op, outcome string }
	counts := map[lp]float64{}
	for _, mf := range mfs {
		if mf.GetName() != "test_auth_ops_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var op, outcome string
			for _, l := range m.GetLabel() {
				switch l.GetName() {
				case "op":
					op = l.GetValue()
				case "outcome":
					outcome = l.GetValue()
				}
			}
			counts[lp{op, outcome}] = m.GetCounter().GetValue()
		}
	}
	for _, tc := range []struct{ op, outcome string }{
		{"magic_start", "ok"}, {"magic_verify", "invalid_token"},
		{"logout", "error"}, {"link_device", "bad_request"},
	} {
		if got := counts[lp{tc.op, tc.outcome}]; got != 1 {
			t.Errorf("op=%s outcome=%s: want 1, got %.0f", tc.op, tc.outcome, got)
		}
	}
}

func TestObserver_DurationHistogramPopulated(t *testing.T) {
	reg := prometheus.NewRegistry()
	obs := promobs.New(reg, "test")
	obs.Observe(identity.OpMagicStart, identity.OutcomeOK, 42*time.Millisecond)
	obs.Observe(identity.OpMagicStart, identity.OutcomeRateLimited, 7*time.Millisecond)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var n uint64
	for _, mf := range mfs {
		if mf.GetName() != "test_auth_op_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "op" && l.GetValue() == "magic_start" {
					n += m.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	if n != 2 {
		t.Errorf("want 2 histogram samples, got %d", n)
	}
}
