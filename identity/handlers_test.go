package identity_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/anatolykoptev/go-panel/identity"
	"github.com/anatolykoptev/go-panel/identity/provider"
	"github.com/anatolykoptev/go-panel/identity/provider/magiclink"
	"github.com/anatolykoptev/go-panel/identity/session"
)

// ---- fakes ------------------------------------------------------------------

type upsertCall struct {
	provider string
	uidHash  []byte
	email    string
}

type fakeUserStore struct {
	userID    string
	created   bool
	snap      session.UserSnapshot
	upsertErr error
	snapErr   error
	linkErr   error

	upserts   []upsertCall
	linkEpids []string
	linkUsers []string
}

func (f *fakeUserStore) UpsertIdentity(_ context.Context, prov string, uidHash []byte, email string) (string, bool, error) {
	f.upserts = append(f.upserts, upsertCall{provider: prov, uidHash: append([]byte(nil), uidHash...), email: email})
	return f.userID, f.created, f.upsertErr
}

func (f *fakeUserStore) GetUserSnapshot(_ context.Context, _ string) (session.UserSnapshot, error) {
	return f.snap, f.snapErr
}

func (f *fakeUserStore) LinkDevice(_ context.Context, epid, userID string) error {
	f.linkEpids = append(f.linkEpids, epid)
	f.linkUsers = append(f.linkUsers, userID)
	return f.linkErr
}

type fakeRateLimiter struct {
	allow bool
	err   error
	keys  []string
}

func (f *fakeRateLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, error) {
	f.keys = append(f.keys, key)
	return f.allow, f.err
}

type sentEmail struct{ to, subject, html, text string }

type fakeEmail struct {
	sent []sentEmail
	err  error
}

func (f *fakeEmail) Send(_ context.Context, to, subject, html, text string) error {
	f.sent = append(f.sent, sentEmail{to, subject, html, text})
	return f.err
}

// ---- harness ----------------------------------------------------------------

const (
	testEmail  = "alice@example.com"
	testPepper = "handler-pepper-32-bytes-aaaaaaaa"
	baseURL    = "https://piter.now"
)

type harness struct {
	auth  *identity.PublicAuthenticator
	users *fakeUserStore
	rl    *fakeRateLimiter
	mail  *fakeEmail
	sess  *session.RedisSessionStore
	mr    *miniredis.Miniredis
}

func newHarness(t *testing.T) *harness {
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
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	return &harness{auth: auth, users: users, rl: rl, mail: mail, sess: sess, mr: mr}
}

func postJSON(target, body string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.7:5555"
	return r
}

// linkToken extracts the token query param from the most recent magic email.
func (h *harness) linkToken(t *testing.T) string {
	t.Helper()
	if len(h.mail.sent) == 0 {
		t.Fatal("no email captured")
	}
	text := h.mail.sent[len(h.mail.sent)-1].text
	i := strings.Index(text, "token=")
	if i < 0 {
		t.Fatalf("no token in email text: %q", text)
	}
	rest := text[i+len("token="):]
	if amp := strings.IndexByte(rest, '&'); amp >= 0 {
		rest = rest[:amp]
	}
	if sp := strings.IndexAny(rest, " \r\n"); sp >= 0 {
		rest = rest[:sp]
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatalf("unescape token: %v", err)
	}
	return tok
}

func cookieByName(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ---- MagicStart: no enumeration --------------------------------------------

// TestMagicStartAlways204 locks the no-enumeration property: the start endpoint
// returns 204 for a valid email, a malformed email, AND a rate-limited request —
// never revealing which case occurred. Falsifiability: returning 400 on bad
// input or 429 when throttled fails this test.
func TestMagicStartAlways204(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		allow     bool
		wantSends int
	}{
		{"valid email sends", `{"email":"alice@example.com"}`, true, 1},
		{"malformed email no send", `{"email":"not-an-email"}`, true, 0},
		{"rate limited no send", `{"email":"alice@example.com"}`, false, 0},
		{"empty body no send", `{}`, true, 0},
		// CRLF in the address would smuggle headers; validEmail must reject it so
		// no email is sent. Falsifiable: drop the control-char check and a send
		// occurs (wantSends becomes 1).
		{"crlf injection no send", `{"email":"a@b.co\r\nBcc:x@y.com"}`, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.rl.allow = tc.allow

			rec := httptest.NewRecorder()
			identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", tc.body))

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204 (no enumeration)", rec.Code)
			}
			if len(h.mail.sent) != tc.wantSends {
				t.Fatalf("emails sent = %d, want %d", len(h.mail.sent), tc.wantSends)
			}
		})
	}
}

func TestMagicStartRateLimitsEmailAndIP(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))

	var sawEmailKey, sawIPKey bool
	for _, k := range h.rl.keys {
		if strings.Contains(k, "alice@example.com") {
			sawEmailKey = true
		}
		if strings.Contains(k, "203.0.113.7") {
			sawIPKey = true
		}
	}
	if !sawEmailKey || !sawIPKey {
		t.Fatalf("rate-limit keys = %v, want both per-email and per-IP", h.rl.keys)
	}
}

// TestMagicStartUsesConfigClientIP locks the per-IP throttle seam: a custom
// Config.ClientIP (e.g. an XFF parser behind a proxy) is used for the IP key,
// not r.RemoteAddr. Falsifiability: if the handler hardcoded RemoteAddr, the
// per-IP key would contain 203.0.113.7, not the override.
func TestMagicStartUsesConfigClientIP(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ml, err := magiclink.New(rdb, []byte(testPepper), 10*time.Minute)
	if err != nil {
		t.Fatalf("magiclink.New: %v", err)
	}
	reg := provider.NewRegistry()
	reg.Register(ml)
	rl := &fakeRateLimiter{allow: true}

	auth, err := identity.New(identity.Config{
		Registry: reg, Sessions: session.NewRedisSessionStore(rdb),
		Users:       &fakeUserStore{userID: "u"},
		Email:       &fakeEmail{},
		Hasher:      newHasher(t, testPepper).Hash,
		RateLimiter: rl, Cookie: identity.DefaultCookieConfig(), BaseURL: baseURL,
		EmailRate: identity.RateRule{Limit: 5, Window: time.Minute},
		IPRate:    identity.RateRule{Limit: 20, Window: time.Minute},
		ClientIP:  func(*http.Request) string { return "10.9.9.9" },
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	identity.MagicStartHandler(auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))

	var sawOverride bool
	for _, k := range rl.keys {
		if strings.Contains(k, "10.9.9.9") {
			sawOverride = true
		}
		if strings.Contains(k, "203.0.113.7") {
			t.Fatalf("per-IP key used RemoteAddr, not Config.ClientIP: %v", rl.keys)
		}
	}
	if !sawOverride {
		t.Fatalf("Config.ClientIP override not used; keys = %v", rl.keys)
	}
}

func TestMagicStartWrongMethod(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/magic/start", nil)
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET start status = %d, want 405", rec.Code)
	}
}

// ---- MagicVerify ------------------------------------------------------------

func TestMagicVerifySetsCookieAndRedirects(t *testing.T) {
	h := newHarness(t)

	// Start to obtain a real token via the emailed link.
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.linkToken(t)

	rec = httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token)+"&return=/dashboard", nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", loc)
	}
	c := cookieByName(rec.Result(), identity.DefaultCookieName)
	if c == nil || c.Value == "" {
		t.Fatal("no session cookie set on verify")
	}
	// Session must exist in the store.
	if _, err := h.sess.Get(context.Background(), c.Value); err != nil {
		t.Fatalf("session not created: %v", err)
	}
}

// TestMagicVerifyHashesEmailWithPepper locks the ADR-002 wiring: the identity
// LOOKUP KEY handed to the store is HMAC-SHA256(email, pepper), not a plaintext
// key, while the raw email is passed separately as the user's contact address.
// Falsifiability: if the handler passed the raw email bytes as the key, the
// hash comparison fails; if it stopped forwarding the contact email, the email
// assertion fails.
func TestMagicVerifyHashesEmailWithPepper(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.linkToken(t)

	rec = httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token), nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	if len(h.users.upserts) != 1 {
		t.Fatalf("UpsertIdentity calls = %d, want 1", len(h.users.upserts))
	}
	got := h.users.upserts[0]
	if got.provider != "email" {
		t.Fatalf("provider = %q, want email", got.provider)
	}
	want := newHasher(t, testPepper).Hash([]byte(testEmail))
	if !bytes.Equal(got.uidHash, want) {
		t.Fatalf("uidHash = %x, want HMAC %x (the identity lookup key stays hashed)", got.uidHash, want)
	}
	if got.email != testEmail {
		t.Fatalf("store contact email = %q, want raw %q (persisted plaintext per operator decision)", got.email, testEmail)
	}
	if bytes.Contains(got.uidHash, []byte(testEmail)) {
		t.Fatal("raw email present in uidHash bytes")
	}
}

// TestMagicVerifyRotatesSession locks session-fixation defense: an existing
// session cookie is revoked and replaced with a new sid. Falsifiability: if the
// handler reused the old sid (no rotation) the new cookie would equal the old and
// the old session would survive.
func TestMagicVerifyRotatesSession(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	oldSID, err := h.sess.Create(ctx, h.users.snap, time.Hour)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.linkToken(t)

	rec = httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token), nil)
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: oldSID})
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	c := cookieByName(rec.Result(), identity.DefaultCookieName)
	if c == nil || c.Value == oldSID {
		t.Fatalf("session not rotated: new cookie = %v", c)
	}
	if _, err := h.sess.Get(ctx, oldSID); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("old session survived rotation: %v", err)
	}
}

// TestMagicVerifyRejectsOpenRedirect locks the open-redirect guard: an absolute
// or protocol-relative return target is replaced by "/". Falsifiability: echoing
// the raw return value would redirect to the attacker host.
func TestMagicVerifyRejectsOpenRedirect(t *testing.T) {
	for _, bad := range []string{"https://evil.com/phish", "//evil.com", "/\\evil.com", "http://evil.com"} {
		h := newHarness(t)
		rec := httptest.NewRecorder()
		identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
		token := h.linkToken(t)

		rec = httptest.NewRecorder()
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
			"/auth/magic/verify?token="+url.QueryEscape(token)+"&return="+url.QueryEscape(bad), nil)
		identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

		loc := rec.Header().Get("Location")
		if loc != "/" {
			t.Fatalf("return=%q → Location=%q, want / (open-redirect blocked)", bad, loc)
		}
	}
}

func TestMagicVerifyInvalidTokenNoCookie(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/magic/verify?token=garbage", nil)
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if c := cookieByName(rec.Result(), identity.DefaultCookieName); c != nil && c.Value != "" {
		t.Fatalf("cookie set for invalid token: %v", c)
	}
	if len(h.users.upserts) != 0 {
		t.Fatal("UpsertIdentity called for invalid token")
	}
}

func TestMagicVerifyLinksDeviceWhenEpidPresent(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	identity.MagicStartHandler(h.auth).ServeHTTP(rec, postJSON("/auth/magic/start", `{"email":"alice@example.com"}`))
	token := h.linkToken(t)

	rec = httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/magic/verify?token="+url.QueryEscape(token), nil)
	r.AddCookie(&http.Cookie{Name: identity.DefaultDeviceCookieName, Value: "epid-xyz"})
	identity.MagicVerifyHandler(h.auth).ServeHTTP(rec, r)

	if len(h.users.linkEpids) != 1 || h.users.linkEpids[0] != "epid-xyz" {
		t.Fatalf("LinkDevice epids = %v, want [epid-xyz]", h.users.linkEpids)
	}
}

// ---- Logout -----------------------------------------------------------------

func TestLogoutExpiresCookieAndRevokes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sid, err := h.sess.Create(ctx, h.users.snap, time.Hour)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: sid})
	identity.LogoutHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	c := cookieByName(rec.Result(), identity.DefaultCookieName)
	if c == nil || c.MaxAge >= 0 {
		t.Fatalf("logout cookie not expired: %v", c)
	}
	if _, err := h.sess.Get(ctx, sid); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("session not revoked on logout: %v", err)
	}
}

// ---- LinkDevice -------------------------------------------------------------

func TestLinkDeviceRequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	identity.LinkDeviceHandler(h.auth).ServeHTTP(rec, postJSON("/auth/device/link", `{"epid":"x"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated LinkDevice status = %d, want 401", rec.Code)
	}
	if len(h.users.linkEpids) != 0 {
		t.Fatal("LinkDevice called without auth")
	}
}

func TestLinkDeviceAuthenticated(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sid, _ := h.sess.Create(ctx, h.users.snap, time.Hour)

	rec := httptest.NewRecorder()
	r := postJSON("/auth/device/link", `{"epid":"epid-42"}`)
	r.AddCookie(&http.Cookie{Name: identity.DefaultCookieName, Value: sid})
	identity.LinkDeviceHandler(h.auth).ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(h.users.linkEpids) != 1 || h.users.linkEpids[0] != "epid-42" {
		t.Fatalf("linkEpids = %v, want [epid-42]", h.users.linkEpids)
	}
	if h.users.linkUsers[0] != "user-1" {
		t.Fatalf("linked user = %q, want user-1", h.users.linkUsers[0])
	}
}
