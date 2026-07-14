package resource_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/pquerna/otp/totp"
)

// ── fixture: totpTestStore extends testAccountStore (nav_filter_test.go)
// with a real (not no-op) in-memory TOTPStore, so this phase's tests can
// drive genuine enroll/confirm/replay/disable behavior over real HTTP
// requests through the full MountPage/guard/CSRF stack. Kept as its own
// type -- distinct from testAccountStore's many pre-existing callers -- so
// nothing here can perturb an unrelated nav/role test's fixture. ──────────

type totpTestStore struct {
	*testAccountStore
	pending  map[string][]byte
	lastStep map[string]int64
	recovery map[string]map[string]bool
}

func newTOTPTestStore() *totpTestStore {
	return &totpTestStore{
		testAccountStore: newTestAccountStore(),
		pending:          map[string][]byte{},
		lastStep:         map[string]int64{},
		recovery:         map[string]map[string]bool{},
	}
}

func (s *totpTestStore) SetPendingTOTPSecret(_ context.Context, id string, encrypted []byte) error {
	a, ok := s.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	s.pending[id] = encrypted
	a.TOTPEnabled = false
	return nil
}

func (s *totpTestStore) ConfirmTOTPEnrollment(_ context.Context, id string) error {
	a, ok := s.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	a.TOTPEnabled = true
	return nil
}

func (s *totpTestStore) GetTOTPSecret(_ context.Context, id string) ([]byte, error) {
	if _, ok := s.byID[id]; !ok {
		return nil, auth.ErrAccountNotFound
	}
	enc, ok := s.pending[id]
	if !ok {
		return nil, auth.ErrTOTPNotEnrolled
	}
	return enc, nil
}

func (s *totpTestStore) DisableTOTP(_ context.Context, id string) error {
	a, ok := s.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	delete(s.pending, id)
	delete(s.lastStep, id)
	delete(s.recovery, id)
	a.TOTPEnabled = false
	return nil
}

func (s *totpTestStore) ConsumeTOTPStep(_ context.Context, id string, step int64) (bool, error) {
	last, has := s.lastStep[id]
	if has && step <= last {
		return false, nil
	}
	s.lastStep[id] = step
	return true, nil
}

func (s *totpTestStore) StoreRecoveryCodes(_ context.Context, id string, hashes [][]byte) error {
	m := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		m[string(h)] = false
	}
	s.recovery[id] = m
	return nil
}

func (s *totpTestStore) ConsumeRecoveryCode(_ context.Context, id string, hash []byte) (bool, error) {
	m, ok := s.recovery[id]
	if !ok {
		return false, nil
	}
	used, exists := m[string(hash)]
	if !exists || used {
		return false, nil
	}
	m[string(hash)] = true
	return true, nil
}

var _ auth.TOTPStore = (*totpTestStore)(nil)

const (
	totpTestIssuer = "go-panel-test"
	totpTestPrefix = "security/totp"
)

var totpTestEncKey = bytes.Repeat([]byte("k"), auth.TOTPEncryptionKeyLen)
var totpTestCSRFKey = bytes.Repeat([]byte("c"), 32)

type testRL struct{}

func (testRL) Allow(context.Context, string, int, time.Duration) (bool, error) { return true, nil }

var testRateLimiter = testRL{}
var testTOTPRate = auth.RateRule{Limit: 10, Window: time.Minute}
var testLoginRate = auth.RateRule{Limit: 10, Window: time.Minute}

// newTOTPTestPanel builds a BcryptTOTPAuth-backed Panel with
// MountTOTPEnrollment wired -- unlike newBcryptPanelWithStore
// (nav_filter_test.go), this ALSO sets Config.CSRFKey (required by
// MountTOTPEnrollment's own fail-closed check) and
// BcryptConfig.TOTPEncryptionKey (required by NewBcryptTOTPAuth itself,
// since store implements TOTPStore -- the SAME key value
// TOTPEnrollmentConfig.TOTPEncryptionKey uses below: both encrypt/decrypt
// the identical totp_secret column via the identical functions, so a real
// deployment must pass the same bytes to both, not two independent keys).
func newTOTPTestPanel(store *totpTestStore) (*resource.Panel, *auth.BcryptTOTPAuth) {
	a := auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:             store,
		HMACKey:           []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:          "/admin",
		SessionTTL:        time.Hour,
		TOTPEncryptionKey: totpTestEncKey,
		RateLimiter:       testRateLimiter,
		LoginRate:         testLoginRate,
		TOTPRate:          testTOTPRate,
	})
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
		CSRFKey:  totpTestCSRFKey,
	})
	resource.MountTOTPEnrollment(p, resource.TOTPEnrollmentConfig{
		Store:             store,
		TOTPEncryptionKey: totpTestEncKey,
		Issuer:            totpTestIssuer,
		PathPrefix:        totpTestPrefix,
	})
	return p, a
}

func totpGet(t *testing.T, p *resource.Panel, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

func totpPost(t *testing.T, p *resource.Panel, cookie *http.Cookie, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// validTOTPToken issues a real CSRF token bound to cookie's value, the same
// way enrollStart's rendered form does (csrf.Issue with the panel's own
// key) -- avoids scraping the token out of rendered HTML.
func validTOTPToken(cookie *http.Cookie) string {
	return csrf.Issue(totpTestCSRFKey, cookie.Value, csrf.DefaultTTL)
}

// enrollAndGetSecret drives GET /enroll and pulls the manual-entry secret
// text out of the response body (it is rendered inside a <code>...</code>
// element with nothing else matching that shape on the page).
func enrollAndGetSecret(t *testing.T, p *resource.Panel, cookie *http.Cookie) string {
	t.Helper()
	w := totpGet(t, p, cookie, "/admin/"+totpTestPrefix+"/enroll/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET enroll: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	start := strings.Index(body, "<code")
	if start < 0 {
		t.Fatalf("enroll page has no <code> secret element: %s", body)
	}
	open := strings.Index(body[start:], ">") + start + 1
	end := strings.Index(body[open:], "</code>") + open
	secret := strings.TrimSpace(body[open:end])
	if secret == "" {
		t.Fatalf("extracted empty secret from enroll page: %s", body)
	}
	return secret
}

func validCodeForSecret(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	return code
}

// ── tests ────────────────────────────────────────────────────────────────

func TestTOTPEnroll_RendersSecretURIAndQRImg(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")

	w := totpGet(t, p, cookie, "/admin/"+totpTestPrefix+"/enroll/")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `src="/admin/`+totpTestPrefix+`/qr.png/"`) {
		t.Errorf("expected an <img> pointing at the qr.png endpoint, got: %s", body)
	}
	if !strings.Contains(body, "otpauth://") {
		t.Error("expected the otpauth:// URI text fallback somewhere on the page")
	}
	if !strings.Contains(body, `name="code"`) {
		t.Error("expected the code-confirmation form's input")
	}
}

func TestTOTPQRImage_ServesValidPNG(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")

	// Must enroll first -- qr.png 404s with nothing pending.
	enrollAndGetSecret(t, p, cookie)

	w := totpGet(t, p, cookie, "/admin/"+totpTestPrefix+"/qr.png/")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG")) {
		t.Error("response body is not a PNG")
	}
}

func TestTOTPQRImage_NotEnrolledYet_404s(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")

	w := totpGet(t, p, cookie, "/admin/"+totpTestPrefix+"/qr.png/")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 before any enrollment started, got %d", w.Code)
	}
}

func TestTOTPConfirm_CorrectCode_EnablesAndShowsRecoveryCodesOnce(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	secret := enrollAndGetSecret(t, p, cookie)
	now := time.Now()

	w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/confirm/", url.Values{
		"code":  {validCodeForSecret(t, secret, now)},
		"_csrf": {validTOTPToken(cookie)},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !store.byID["u1"].TOTPEnabled {
		t.Error("TOTPEnabled must be true after a correct confirm")
	}
	if len(store.recovery["u1"]) != auth.RecoveryCodeCount {
		t.Errorf("expected %d recovery codes stored, got %d", auth.RecoveryCodeCount, len(store.recovery["u1"]))
	}
	// The plaintext codes must appear in THIS response -- the one and only
	// place they are ever shown.
	body := w.Body.String()
	codeCount := strings.Count(body, `<li style="user-select:all">`)
	if codeCount != auth.RecoveryCodeCount {
		t.Errorf("expected %d recovery codes rendered in the response, found %d <li> entries", auth.RecoveryCodeCount, codeCount)
	}
}

func TestTOTPConfirm_WrongCode_StaysDisabled(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	enrollAndGetSecret(t, p, cookie)

	w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/confirm/", url.Values{
		"code":  {"000000"},
		"_csrf": {validTOTPToken(cookie)},
	})
	if w.Code != http.StatusOK { // re-rendered enroll page, not an error status
		t.Fatalf("expected 200 (re-rendered enroll page), got %d", w.Code)
	}
	if store.byID["u1"].TOTPEnabled {
		t.Error("TOTPEnabled must stay false after a wrong code")
	}
	if len(store.recovery["u1"]) != 0 {
		t.Error("no recovery codes must be generated after a wrong code")
	}
}

// TestTOTPConfirm_ReplayedCode_SecondAttemptRejected is the required
// end-to-end proof that P3's replay guard (ConsumeTOTPStep) actually
// engages over a live HTTP path, not just in the auth package's own unit
// tests.
func TestTOTPConfirm_ReplayedCode_SecondAttemptRejected(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	secret := enrollAndGetSecret(t, p, cookie)
	now := time.Now()
	code := validCodeForSecret(t, secret, now)
	form := url.Values{"code": {code}, "_csrf": {validTOTPToken(cookie)}}

	first := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/confirm/", form)
	if first.Code != http.StatusOK || !store.byID["u1"].TOTPEnabled {
		t.Fatalf("first confirm: expected success, got code=%d enabled=%v", first.Code, store.byID["u1"].TOTPEnabled)
	}

	// The account is now enrolled, so a second POST refuses via the
	// already-enabled guard before ever re-checking the code -- reset the
	// flag to isolate the replay-guard assertion specifically (this test's
	// actual subject), matching the same isolation the auth-package unit
	// test uses.
	store.byID["u1"].TOTPEnabled = false
	second := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/confirm/", form)
	if second.Code != http.StatusOK { // re-rendered enroll page with an error, not enabled
		t.Fatalf("replayed confirm: expected 200 (rejected, re-rendered), got %d", second.Code)
	}
	if store.byID["u1"].TOTPEnabled {
		t.Error("a replayed code must not be able to (re-)enable TOTP")
	}
}

func TestTOTPDisable_WithoutReauth_StaysEnabled(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	store.byID["u1"].TOTPEnabled = true

	w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/disable/", url.Values{
		"current_password": {"totally-wrong"},
		"_csrf":            {validTOTPToken(cookie)},
	})
	if w.Code != http.StatusOK { // re-rendered re-auth form, not an error status
		t.Fatalf("expected 200 (re-rendered form), got %d", w.Code)
	}
	if !store.byID["u1"].TOTPEnabled {
		t.Error("TOTPEnabled must stay true when re-auth fails")
	}
}

func TestTOTPDisable_WithCorrectReauth_Disables(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	store.byID["u1"].TOTPEnabled = true
	store.pending["u1"] = []byte("some-encrypted-secret")
	store.recovery["u1"] = map[string]bool{"h": false}

	w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/disable/", url.Values{
		"current_password": {"pw"},
		"_csrf":            {validTOTPToken(cookie)},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.byID["u1"].TOTPEnabled {
		t.Error("TOTPEnabled must be false after a correctly re-authenticated disable")
	}
	if len(store.recovery["u1"]) != 0 {
		t.Error("recovery codes must be cleared on disable")
	}
}

func TestTOTPRegenerate_WrongPassword_KeepsOldCodes(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	store.byID["u1"].TOTPEnabled = true
	store.recovery["u1"] = map[string]bool{"original-hash": false}

	w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/regenerate/", url.Values{
		"current_password": {"totally-wrong"},
		"_csrf":            {validTOTPToken(cookie)},
	})
	if w.Code != http.StatusOK { // re-rendered re-auth form
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if _, ok := store.recovery["u1"]["original-hash"]; !ok {
		t.Fatal("the original recovery-code set must survive a failed re-auth")
	}
}

func TestTOTPRegenerate_WithCorrectReauth_ReplacesCodesAndShowsThemOnce(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	store.byID["u1"].TOTPEnabled = true
	store.recovery["u1"] = map[string]bool{"original-hash": false}

	w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/regenerate/", url.Values{
		"current_password": {"pw"},
		"_csrf":            {validTOTPToken(cookie)},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if _, stillThere := store.recovery["u1"]["original-hash"]; stillThere {
		t.Fatal("the old recovery-code set must be replaced, not merged")
	}
	if got := len(store.recovery["u1"]); got != auth.RecoveryCodeCount {
		t.Fatalf("expected %d new codes stored, got %d", auth.RecoveryCodeCount, got)
	}
	if codeCount := strings.Count(w.Body.String(), `<li style="user-select:all">`); codeCount != auth.RecoveryCodeCount {
		t.Errorf("expected %d new codes rendered in the response, found %d", auth.RecoveryCodeCount, codeCount)
	}
}

func TestTOTPDisable_GET_ShowsFormButDoesNotDisable(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	store.byID["u1"].TOTPEnabled = true

	w := totpGet(t, p, cookie, "/admin/"+totpTestPrefix+"/disable/")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `name="current_password"`) {
		t.Error("expected the password re-auth form")
	}
	if !store.byID["u1"].TOTPEnabled {
		t.Error("a bare GET must never disable 2FA")
	}
}

func TestTOTPCSRF_PostWithoutValidToken_Rejected(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, a := newTOTPTestPanel(store)
	cookie := bcryptLogin(t, a, "op@example.com", "pw")
	secret := enrollAndGetSecret(t, p, cookie)

	cases := map[string]string{
		"missing token": "",
		"garbage token": "not-a-real-token",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			form := url.Values{"code": {validCodeForSecret(t, secret, time.Now())}}
			if tok != "" {
				form.Set("_csrf", tok)
			}
			w := totpPost(t, p, cookie, "/admin/"+totpTestPrefix+"/confirm/", form)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s: expected 403, got %d", name, w.Code)
			}
			if store.byID["u1"].TOTPEnabled {
				t.Fatalf("%s: TOTP must not be enabled without valid CSRF", name)
			}
		})
	}
}

func TestTOTPAnonymous_EveryRouteDeniedByGuard(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	p, _ := newTOTPTestPanel(store)

	routes := []struct {
		method, path string
	}{
		{http.MethodGet, "/admin/" + totpTestPrefix + "/enroll/"},
		{http.MethodGet, "/admin/" + totpTestPrefix + "/qr.png/"},
		{http.MethodPost, "/admin/" + totpTestPrefix + "/confirm/"},
		{http.MethodGet, "/admin/" + totpTestPrefix + "/disable/"},
		{http.MethodPost, "/admin/" + totpTestPrefix + "/disable/"},
		{http.MethodGet, "/admin/" + totpTestPrefix + "/regenerate/"},
		{http.MethodPost, "/admin/" + totpTestPrefix + "/regenerate/"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), rt.method, rt.path, nil)
			w := httptest.NewRecorder()
			p.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("expected 303 redirect to login (guard denial), got %d", w.Code)
			}
		})
	}
	if len(store.pending) != 0 || len(store.recovery) != 0 {
		t.Fatal("an anonymous request must never reach a handler that touches TOTP state")
	}
}

// TestTOTPAccountScoping_CannotActOnAnotherAccount proves the design
// invariant the spec calls out: since NO handler ever reads an account ID
// from the request (only auth.SessionFrom), there is no field to smuggle a
// different account's ID through in the first place -- this test smuggles
// one anyway (an "account_id" form field pointing at account B) while
// authenticated as A, and confirms it is silently ignored: A's own action
// runs, B's TOTP state never changes.
func TestTOTPAccountScoping_CannotActOnAnotherAccount(t *testing.T) {
	store := newTOTPTestStore()
	seedAccount(t, store.testAccountStore, "acct-a", "a@example.com", "pw-a", "admin")
	seedAccount(t, store.testAccountStore, "acct-b", "b@example.com", "pw-b", "admin")
	store.byID["acct-b"].TOTPEnabled = true
	store.pending["acct-b"] = []byte("bs-encrypted-secret")
	p, a := newTOTPTestPanel(store)
	cookieA := bcryptLogin(t, a, "a@example.com", "pw-a")

	// A enrolls -- must only ever touch acct-a's row.
	secretA := enrollAndGetSecret(t, p, cookieA)
	if _, touched := store.pending["acct-a"]; !touched {
		t.Fatal("expected A's own pending secret to be set")
	}
	if !bytes.Equal(store.pending["acct-b"], []byte("bs-encrypted-secret")) {
		t.Fatal("A's enroll must not touch B's stored secret")
	}

	// A confirms, smuggling account_id=acct-b in the POST body.
	w := totpPost(t, p, cookieA, "/admin/"+totpTestPrefix+"/confirm/", url.Values{
		"code":       {validCodeForSecret(t, secretA, time.Now())},
		"_csrf":      {validTOTPToken(cookieA)},
		"account_id": {"acct-b"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !store.byID["acct-a"].TOTPEnabled {
		t.Fatal("A's own confirm must have enabled A's TOTP")
	}
	// B was already enabled before this test's confirm call -- the
	// assertion that matters is that B's step/recovery state was NEVER
	// touched by A's action.
	if len(store.recovery["acct-b"]) != 0 {
		t.Fatal("A's confirm must never write recovery codes for B")
	}

	// A disables, again smuggling account_id=acct-b.
	w = totpPost(t, p, cookieA, "/admin/"+totpTestPrefix+"/disable/", url.Values{
		"current_password": {"pw-a"},
		"_csrf":            {validTOTPToken(cookieA)},
		"account_id":       {"acct-b"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.byID["acct-a"].TOTPEnabled {
		t.Fatal("A's own disable must have disabled A's TOTP")
	}
	if !store.byID["acct-b"].TOTPEnabled {
		t.Fatal("B's TOTP must be UNCHANGED by A's disable call, regardless of a smuggled account_id field")
	}
	if !bytes.Equal(store.pending["acct-b"], []byte("bs-encrypted-secret")) {
		t.Fatal("B's secret must be UNCHANGED by A's disable call")
	}
}
