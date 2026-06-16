package identity_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/anatolykoptev/go-panel/identity"
	"github.com/anatolykoptev/go-panel/identity/provider"
	"github.com/anatolykoptev/go-panel/identity/provider/magiclink"
	"github.com/anatolykoptev/go-panel/identity/session"
)

// ---- fakeObserver -----------------------------------------------------------

type obsCall struct {
	op      identity.Op
	outcome identity.Outcome
}

// recordingObserver captures every (op, outcome) pair the handlers emit.
// Concurrency-safe: handlers may call Observe from any goroutine.
type recordingObserver struct {
	mu   sync.Mutex
	seen []obsCall
}

func (o *recordingObserver) Observe(op identity.Op, outcome identity.Outcome, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, obsCall{op, outcome})
}

func (o *recordingObserver) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = nil
}

// assertOne asserts exactly one observation with the given op and outcome.
func (o *recordingObserver) assertOne(t *testing.T, wantOp identity.Op, wantOutcome identity.Outcome) {
	t.Helper()
	o.mu.Lock()
	calls := make([]obsCall, len(o.seen))
	copy(calls, o.seen)
	o.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("observer: got %d calls, want 1: %+v", len(calls), calls)
	}
	if calls[0].op != wantOp || calls[0].outcome != wantOutcome {
		t.Fatalf("observer: got (%v,%v), want (%v,%v)", calls[0].op, calls[0].outcome, wantOp, wantOutcome)
	}
}

// ---- obsHarness: newHarness + recordingObserver ----------------------------

type obsHarness struct {
	auth  *identity.PublicAuthenticator
	users *fakeUserStore
	rl    *fakeRateLimiter
	mail  *fakeEmail
	sess  *session.RedisSessionStore
	mr    *miniredis.Miniredis
	obs   *recordingObserver
}

// newObsHarness mirrors newHarness but wires a recordingObserver via
// Config.Observer. All token storage uses the same miniredis so start→verify
// round-trips resolve correctly.
func newObsHarness(t *testing.T) *obsHarness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	ml, err := magiclink.New(rdb, []byte(testPepper), 10*time.Minute)
	if err != nil {
		t.Fatalf("magiclink.New: %v", err)
	}
	reg := provider.NewRegistry()
	reg.Register(ml)

	sess := session.NewRedisSessionStore(rdb)
	users := &fakeUserStore{
		userID:  "user-1",
		created: true,
		snap: session.UserSnapshot{
			UserID: "user-1", DisplayName: "Alice", CitySlug: "spb",
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	rl := &fakeRateLimiter{allow: true}
	mail := &fakeEmail{}
	obs := &recordingObserver{}

	auth, err := identity.New(identity.Config{
		Registry:    reg,
		Sessions:    sess,
		Users:       users,
		Email:       mail,
		Hasher:      newHasher(t, testPepper).Hash,
		RateLimiter: rl,
		Cookie:      identity.DefaultCookieConfig(),
		BaseURL:     baseURL,
		MagicTTL:    10 * time.Minute,
		SessionTTL:  time.Hour,
		EmailRate:   identity.RateRule{Limit: 5, Window: 15 * time.Minute},
		IPRate:      identity.RateRule{Limit: 20, Window: 15 * time.Minute},
		Observer:    obs,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("identity.New with observer: %v", err)
	}
	return &obsHarness{auth: auth, users: users, rl: rl, mail: mail, sess: sess, mr: mr, obs: obs}
}

// extractToken extracts the magic-link token from the most recently sent email.
func (h *obsHarness) extractToken(t *testing.T) string {
	t.Helper()
	if len(h.mail.sent) == 0 {
		t.Fatal("no email captured")
	}
	text := h.mail.sent[len(h.mail.sent)-1].text
	const needle = "token="
	idx := indexSubstring(text, needle)
	if idx < 0 {
		t.Fatalf("no %q in email body: %q", needle, text)
	}
	rest := text[idx+len(needle):]
	for _, stop := range []byte{'&', ' ', '\r', '\n'} {
		for i := range rest {
			if rest[i] == stop {
				rest = rest[:i]
				break
			}
		}
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatalf("unescape token: %v", err)
	}
	return tok
}

func indexSubstring(s, sub string) int {
	if len(sub) == 0 || len(s) < len(sub) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---- NopObserver: zero-config callers keep working -------------------------

// TestNopObserverIsDefault verifies that identity.New with no Config.Observer
// still constructs successfully and the handlers complete without panicking on
// a nil dereference.
// Falsifiability: if applyDefaults skipped the nil-Observer guard so
// cfg.Observer stayed nil, the first Observe(...) call panics — this test fails.
func TestNopObserverIsDefault(t *testing.T) {
	h := newHarness(t) // newHarness sets no Observer
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("NopObserver default: status = %d, want 204", rec.Code)
	}
}

// ---- MagicStart outcomes ---------------------------------------------------

// TestObserverMagicStartOK verifies that a valid, under-limit start emits
// (OpMagicStart, OutcomeOK).
// Falsifiability: if the OutcomeOK observe call were removed from the success
// path, zero observations are recorded and assertOne fails.
func TestObserverMagicStartOK(t *testing.T) {
	h := newObsHarness(t)
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicStart, identity.OutcomeOK)
}

// TestObserverMagicStartBadRequest verifies that an invalid email emits
// (OpMagicStart, OutcomeBadRequest).
// Falsifiability: removing the BadRequest observe call when validEmail returns
// false yields zero observations; assertOne then fails.
func TestObserverMagicStartBadRequest(t *testing.T) {
	h := newObsHarness(t)
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"not-an-email"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (no enumeration)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicStart, identity.OutcomeBadRequest)
}

// TestObserverMagicStartRateLimited verifies that a throttled attempt emits
// (OpMagicStart, OutcomeRateLimited).
// Falsifiability: if the handler fell through to OutcomeOK despite
// allowStart returning false, assertOne would find the wrong outcome.
func TestObserverMagicStartRateLimited(t *testing.T) {
	h := newObsHarness(t)
	h.rl.allow = false
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (no enumeration)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicStart, identity.OutcomeRateLimited)
}

// ---- MagicVerify outcomes --------------------------------------------------

// TestObserverMagicVerifyOK verifies that a successful verification emits
// (OpMagicVerify, OutcomeOK).
// Falsifiability: changing the verify success observe call to OutcomeError
// causes assertOne to fail on the outcome mismatch.
func TestObserverMagicVerifyOK(t *testing.T) {
	h := newObsHarness(t)

	// Obtain a real token from the start handler.
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.extractToken(t)
	h.obs.reset() // clear the start observation; only verify matters here

	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token), nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicVerify, identity.OutcomeOK)
}

// TestObserverMagicVerifyInvalidToken verifies that a forged or expired token
// emits (OpMagicVerify, OutcomeInvalidToken).
// Falsifiability: if the invalid-token early-return path were removed, the
// handler would proceed to UpsertIdentity and panic (nil id.Email), not emit
// OutcomeInvalidToken — test fails on panic or wrong outcome.
func TestObserverMagicVerifyInvalidToken(t *testing.T) {
	h := newObsHarness(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/magic/verify?token=garbage", nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (redirect to login)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicVerify, identity.OutcomeInvalidToken)
}

// ---- Logout outcome --------------------------------------------------------

// TestObserverLogoutOK verifies that a POST to logout emits
// (OpLogout, OutcomeOK).
// Falsifiability: removing the Observe call in LogoutHandler yields zero
// observations; assertOne reports "got 0 calls".
func TestObserverLogoutOK(t *testing.T) {
	h := newObsHarness(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	identity.LogoutHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	h.obs.assertOne(t, identity.OpLogout, identity.OutcomeOK)
}

// ---- LinkDevice outcomes ---------------------------------------------------

// TestObserverLinkDeviceOK verifies that an authenticated, well-formed link
// request emits (OpLinkDevice, OutcomeOK).
// Falsifiability: removing the OutcomeOK observe call at the success branch
// yields zero observations; assertOne fails.
func TestObserverLinkDeviceOK(t *testing.T) {
	h := newObsHarness(t)
	sid, err := h.sess.Create(context.Background(), h.users.snap, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := httptest.NewRecorder()
	r := postJSON("/auth/device/link", `{"epid":"device-42"}`)
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: sid})
	identity.LinkDeviceHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	h.obs.assertOne(t, identity.OpLinkDevice, identity.OutcomeOK)
}

// TestObserverLinkDeviceNoSession verifies that a missing session cookie emits
// (OpLinkDevice, OutcomeBadRequest) and the handler returns 401.
// Falsifiability: if the unauthenticated path omitted the observe call, zero
// observations are recorded and assertOne fails.
func TestObserverLinkDeviceNoSession(t *testing.T) {
	h := newObsHarness(t)
	rec := httptest.NewRecorder()
	r := postJSON("/auth/device/link", `{"epid":"device-42"}`)
	// no session cookie attached
	identity.LinkDeviceHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	h.obs.assertOne(t, identity.OpLinkDevice, identity.OutcomeBadRequest)
}

// TestNopObserverObserveIsNoOp directly invokes NopObserver.Observe to satisfy
// the interface contract and ensure the method executes without panic.
// Falsifiability: if NopObserver.Observe panicked or returned an error, this
// test would fail. It also keeps coverage above baseline by exercising the one
// new statement added by this PR (the Observe method body).
func TestNopObserverObserveIsNoOp(t *testing.T) {
	var nop identity.NopObserver
	// Must not panic; duration is arbitrary.
	nop.Observe(identity.OpMagicStart, identity.OutcomeOK, time.Millisecond)
}

// TestObserverMagicVerifyUpsertError verifies that an UpsertIdentity failure
// after token verification emits (OpMagicVerify, OutcomeError).
// Falsifiability: removing the OutcomeError observe call on the UpsertIdentity
// error path yields zero observations after the reset; assertOne fails.
func TestObserverMagicVerifyUpsertError(t *testing.T) {
	h := newObsHarness(t)
	h.users.upsertErr = errors.New("db unavailable")

	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.extractToken(t)
	h.obs.reset()

	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token), nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (redirect to login)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicVerify, identity.OutcomeError)
}

// TestObserverLinkDeviceInvalidSession verifies that a present-but-unknown
// session cookie (e.g. expired) emits (OpLinkDevice, OutcomeInvalidToken).
// Falsifiability: if the bad-session branch were missing its observe call,
// zero observations are recorded and assertOne fails.
func TestObserverLinkDeviceInvalidSession(t *testing.T) {
	h := newObsHarness(t)
	rec := httptest.NewRecorder()
	r := postJSON("/auth/device/link", `{"epid":"device-42"}`)
	// Cookie present but session unknown to the store (stale / revoked).
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: "unknown-sid"})
	identity.LinkDeviceHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	h.obs.assertOne(t, identity.OpLinkDevice, identity.OutcomeInvalidToken)
}

// TestObserverMagicStartEmailSendError verifies that an email-send failure emits
// exactly one (OpMagicStart, OutcomeError) and NOT a trailing OutcomeOK. This is
// the load-bearing backstop for the explicit return on the Email.Send error
// branch: if that return regressed, the handler would fall through to the
// OutcomeOK emit and assertOne would see two calls.
func TestObserverMagicStartEmailSendError(t *testing.T) {
	h := newObsHarness(t)
	h.mail.err = errors.New("smtp unavailable")
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (no enumeration)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicStart, identity.OutcomeError)
}

// TestObserverMagicVerifySnapshotError verifies that a GetUserSnapshot failure
// after a successful upsert emits (OpMagicVerify, OutcomeError).
func TestObserverMagicVerifySnapshotError(t *testing.T) {
	h := newObsHarness(t)
	h.users.snapErr = errors.New("snapshot db error")
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.extractToken(t)
	h.obs.reset()
	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token), nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (redirect to login)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpMagicVerify, identity.OutcomeError)
}

// TestObserverLinkDeviceLinkError verifies that a LinkDevice store failure on an
// authenticated request emits (OpLinkDevice, OutcomeError) and returns 500.
func TestObserverLinkDeviceLinkError(t *testing.T) {
	h := newObsHarness(t)
	h.users.linkErr = errors.New("link db error")
	sid, err := h.sess.Create(context.Background(), h.users.snap, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	rec := httptest.NewRecorder()
	r := postJSON("/auth/device/link", `{"epid":"device-42"}`)
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: sid})
	identity.LinkDeviceHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	h.obs.assertOne(t, identity.OpLinkDevice, identity.OutcomeError)
}

// TestObserverLogoutRevokeError verifies that when the server-side session revoke
// fails, logout still expires the cookie and redirects (UX unchanged) but the
// observation is (OpLogout, OutcomeError) so a session-leak logout is visible to
// metrics. Falsifiability: if LogoutHandler hardcoded OutcomeOK, assertOne fails
// on the outcome mismatch.
func TestObserverLogoutRevokeError(t *testing.T) {
	h := newObsHarness(t)
	sid, err := h.sess.Create(context.Background(), h.users.snap, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	h.mr.Close() // backing store down -> Revoke errors
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: sid})
	identity.LogoutHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (logout UX unchanged on revoke failure)", rec.Code)
	}
	h.obs.assertOne(t, identity.OpLogout, identity.OutcomeError)
}
