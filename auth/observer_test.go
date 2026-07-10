package auth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

// observation is one recorded spyObserver.Observe call.
type observation struct {
	op      auth.Op
	outcome auth.Outcome
	dur     time.Duration
}

// spyObserver is a concurrency-safe auth.Observer test double that records
// every call for assertion.
type spyObserver struct {
	mu    sync.Mutex
	calls []observation
}

func (s *spyObserver) Observe(op auth.Op, outcome auth.Outcome, dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, observation{op: op, outcome: outcome, dur: dur})
}

func (s *spyObserver) snapshot() []observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]observation, len(s.calls))
	copy(out, s.calls)
	return out
}

// liveSessionCfg builds a BcryptConfig sharing the fixed test HMACKey/BasePath/
// SessionTTL used across this package's tests, with the observer + revocation
// policy under test layered on top.
func liveSessionCfg(store auth.AccountStore, obs auth.Observer, failClosed bool) auth.BcryptConfig {
	return auth.BcryptConfig{
		Store:                store,
		HMACKey:              []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:             "/admin",
		SessionTTL:           time.Hour,
		Observer:             obs,
		RevocationFailClosed: failClosed,
	}
}

// sessionCookieFor logs in and returns the resulting session cookie, or fails
// the test if login did not issue one.
func sessionCookieFor(t *testing.T, a *auth.BcryptTOTPAuth, email, pw string) *http.Cookie {
	t.Helper()
	c := sessionCookie(loginPOST(a, email, pw))
	if c == nil {
		t.Fatal("expected a session cookie after login")
	}
	return c
}

// requireHandlerCalled runs a.Require(next) against a request carrying c and
// reports whether next was invoked (true) or the request was rejected (false).
func requireHandlerCalled(a *auth.BcryptTOTPAuth, c *http.Cookie) (called bool, code int) {
	h := a.Require(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h(w, r)
	return called, w.Code
}

func TestBcrypt_RevocationFailClosed_DeniesOnTransientError(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	spy := &spyObserver{}
	a := auth.NewBcryptTOTPAuth(liveSessionCfg(store, spy, true))
	c := sessionCookieFor(t, a, "op@example.com", "s3cret")

	store.simulateTransientErr(errors.New("connection reset"))

	called, code := requireHandlerCalled(a, c)
	if called {
		t.Fatal("RevocationFailClosed=true: handler must NOT run on a transient GetByID error")
	}
	if code != http.StatusSeeOther {
		t.Fatalf("expected a login redirect (session rejected), got %d", code)
	}

	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 Observe call, got %d: %+v", len(calls), calls)
	}
	if calls[0].op != auth.OpSessionRecheck || calls[0].outcome != auth.OutcomeError {
		t.Fatalf("expected Observe(OpSessionRecheck, OutcomeError, _), got %+v", calls[0])
	}
}

func TestBcrypt_DefaultFailOpen_AllowsButRecordsDegrade(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	spy := &spyObserver{}
	// RevocationFailClosed left at its zero value (false) — must match today's
	// default behavior exactly: the crypto-valid session survives a transient
	// store error.
	a := auth.NewBcryptTOTPAuth(liveSessionCfg(store, spy, false))
	c := sessionCookieFor(t, a, "op@example.com", "s3cret")

	store.simulateTransientErr(errors.New("connection reset"))

	called, code := requireHandlerCalled(a, c)
	if !called || code != http.StatusOK {
		t.Fatalf("default fail-open must still allow the crypto-valid session, got called=%v code=%d", called, code)
	}

	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 Observe call recording the degrade, got %d: %+v", len(calls), calls)
	}
	if calls[0].op != auth.OpSessionRecheck || calls[0].outcome != auth.OutcomeError {
		t.Fatalf("expected Observe(OpSessionRecheck, OutcomeError, _), got %+v", calls[0])
	}
}

func TestBcrypt_NilObserver_NoPanic(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	// Observer left nil — NewBcryptTOTPAuth must default it to a NopObserver.
	a := auth.NewBcryptTOTPAuth(liveSessionCfg(store, nil, false))
	c := sessionCookieFor(t, a, "op@example.com", "s3cret")

	store.simulateTransientErr(errors.New("connection reset"))

	called, code := requireHandlerCalled(a, c)
	if !called || code != http.StatusOK {
		t.Fatalf("nil Observer must not change fail-open behavior, got called=%v code=%d", called, code)
	}
}
