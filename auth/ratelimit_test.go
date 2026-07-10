package auth_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

// fakeLimiterCall records one Allow invocation for assertion.
type fakeLimiterCall struct {
	key    string
	limit  int
	window time.Duration
}

// fakeLimiter is a scriptable auth.RateLimiter test double: allow/err are
// fixed per test, calls records every invocation.
type fakeLimiter struct {
	allow bool
	err   error
	calls []fakeLimiterCall
}

func (f *fakeLimiter) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	f.calls = append(f.calls, fakeLimiterCall{key: key, limit: limit, window: window})
	return f.allow, f.err
}

// rateLimitCfg builds a BcryptConfig sharing the fixed test HMACKey/BasePath/
// SessionTTL used across this package's tests, with the rate limiter under
// test layered on top.
func rateLimitCfg(store auth.AccountStore, obs auth.Observer, rl auth.RateLimiter, rule auth.RateRule) auth.BcryptConfig {
	return auth.BcryptConfig{
		Store:       store,
		HMACKey:     []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:    "/admin",
		SessionTTL:  time.Hour,
		Observer:    obs,
		RateLimiter: rl,
		LoginRate:   rule,
	}
}

func TestBcrypt_RateLimit_OverQuota_DeniesBeforeBcrypt(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	spy := &spyObserver{}
	limiter := &fakeLimiter{allow: false}
	rule := auth.RateRule{Limit: 5, Window: time.Minute}
	a := auth.NewBcryptTOTPAuth(rateLimitCfg(store, spy, limiter, rule))

	w := loginPOST(a, "op@example.com", "s3cret")

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if got, want := w.Header().Get("Retry-After"), strconv.Itoa(int(rule.Window.Seconds())); got != want {
		t.Fatalf("expected Retry-After %q, got %q", want, got)
	}
	if sessionCookie(w) != nil {
		t.Fatal("over-quota login must not issue a session cookie")
	}
	if store.getByEmailCalls != 0 {
		t.Fatalf("bcrypt/store path must never run when over-quota, GetByEmail called %d times", store.getByEmailCalls)
	}
	calls := spy.snapshot()
	if len(calls) != 1 || calls[0].op != auth.OpBcryptLogin || calls[0].outcome != auth.OutcomeRateLimited {
		t.Fatalf("expected exactly Observe(OpBcryptLogin, OutcomeRateLimited, _), got %+v", calls)
	}
}

func TestBcrypt_RateLimiterError_FailsClosed(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	spy := &spyObserver{}
	limiter := &fakeLimiter{err: errors.New("redis: connection refused")}
	rule := auth.RateRule{Limit: 5, Window: 30 * time.Second}
	a := auth.NewBcryptTOTPAuth(rateLimitCfg(store, spy, limiter, rule))

	w := loginPOST(a, "op@example.com", "s3cret")

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("a limiter error must fail CLOSED (429), got %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("expected Retry-After 30, got %q", got)
	}
	if store.getByEmailCalls != 0 {
		t.Fatalf("bcrypt/store path must never run on a limiter error, GetByEmail called %d times", store.getByEmailCalls)
	}
	calls := spy.snapshot()
	if len(calls) != 1 || calls[0].op != auth.OpBcryptLogin || calls[0].outcome != auth.OutcomeLimiterError {
		t.Fatalf("expected exactly Observe(OpBcryptLogin, OutcomeLimiterError, _), got %+v", calls)
	}
}

// TestBcrypt_NilRateLimiter_NoThrottle proves the additive/non-breaking
// contract: with RateLimiter left nil (the zero value, pre-Phase-2 default),
// login behaves exactly as before — a correct password issues a session, a
// wrong one still 401s — while the new login-outcome instrumentation still
// fires (nil-safe, additive metrics do not gate behavior).
func TestBcrypt_NilRateLimiter_NoThrottle(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	spy := &spyObserver{}
	a := auth.NewBcryptTOTPAuth(rateLimitCfg(store, spy, nil, auth.RateRule{}))

	wOK := loginPOST(a, "op@example.com", "s3cret")
	if wOK.Code != http.StatusSeeOther || sessionCookie(wOK) == nil {
		t.Fatalf("nil RateLimiter: correct credentials must still issue a session, got code=%d cookie=%v", wOK.Code, sessionCookie(wOK))
	}

	wBad := loginPOST(a, "op@example.com", "wrong")
	if wBad.Code != http.StatusUnauthorized {
		t.Fatalf("nil RateLimiter: wrong password must still be 401, got %d", wBad.Code)
	}

	calls := spy.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 Observe calls (OK then InvalidCredentials), got %d: %+v", len(calls), calls)
	}
	if calls[0].op != auth.OpBcryptLogin || calls[0].outcome != auth.OutcomeOK {
		t.Fatalf("expected first Observe(OpBcryptLogin, OutcomeOK, _), got %+v", calls[0])
	}
	if calls[1].op != auth.OpBcryptLogin || calls[1].outcome != auth.OutcomeInvalidCredentials {
		t.Fatalf("expected second Observe(OpBcryptLogin, OutcomeInvalidCredentials, _), got %+v", calls[1])
	}
}

// TestBcrypt_RateLimit_SubSecondWindow_RetryAfterFloorsAtOne proves
// rejectThrottled never renders a nonsensical "retry immediately" Retry-After:
// int(Window.Seconds()) truncates a sub-second Window to 0, so the header
// must floor at 1.
func TestBcrypt_RateLimit_SubSecondWindow_RetryAfterFloorsAtOne(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	limiter := &fakeLimiter{allow: false}
	rule := auth.RateRule{Limit: 5, Window: 500 * time.Millisecond}
	a := auth.NewBcryptTOTPAuth(rateLimitCfg(store, nil, limiter, rule))

	w := loginPOST(a, "op@example.com", "s3cret")

	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After floored at 1 for a sub-second window, got %q", got)
	}
}

func TestBcrypt_RateLimiterSet_ZeroLoginRate_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic when RateLimiter is set but LoginRate is zero")
		}
	}()
	auth.NewBcryptTOTPAuth(rateLimitCfg(newFakeStore(), nil, &fakeLimiter{allow: true}, auth.RateRule{}))
}
