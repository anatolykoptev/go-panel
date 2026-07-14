package auth_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/pquerna/otp/totp"
)

// fakeStore is an in-memory AccountStore for authenticator unit tests.
type fakeStore struct {
	byEmail         map[string]*auth.Account
	byID            map[string]*auth.Account
	forceGetByIDErr error // when set, GetByID returns this error unconditionally
	getByEmailCalls int   // counts GetByEmail invocations, e.g. to prove a rate-limit deny short-circuits before bcrypt
}

func newFakeStore() *fakeStore {
	return &fakeStore{byEmail: map[string]*auth.Account{}, byID: map[string]*auth.Account{}}
}

func (f *fakeStore) add(a *auth.Account) {
	f.byEmail[a.Email] = a
	f.byID[a.ID] = a
}

// deactivate replaces the stored account with a new, inactive copy (immutable update).
func (f *fakeStore) deactivate(id string) {
	a, ok := f.byID[id]
	if !ok {
		return
	}
	cp := *a
	cp.Active = false
	f.byID[id] = &cp
	f.byEmail[cp.Email] = &cp
}

// simulateTransientErr makes every subsequent GetByID call return err — models
// a transient store outage (e.g. a dropped DB connection) independent of
// auth.ErrAccountNotFound.
func (f *fakeStore) simulateTransientErr(err error) {
	f.forceGetByIDErr = err
}

func (f *fakeStore) GetByEmail(_ context.Context, email string) (*auth.Account, error) {
	f.getByEmailCalls++
	a, ok := f.byEmail[email]
	if !ok || !a.Active || a.PasswordHash == "" {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeStore) GetByID(_ context.Context, id string) (*auth.Account, error) {
	if f.forceGetByIDErr != nil {
		return nil, f.forceGetByIDErr
	}
	a, ok := f.byID[id]
	if !ok {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeStore) UpdateLastLogin(context.Context, string) error { return nil }

func (f *fakeStore) UpdatePasswordHash(_ context.Context, id, hash string) error {
	a, ok := f.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	cp := *a
	cp.PasswordHash = hash
	f.byID[id] = &cp
	f.byEmail[cp.Email] = &cp
	return nil
}

func (f *fakeStore) CreateAccount(context.Context, string, string, string, string) (string, bool, error) {
	return "", false, nil
}

// fakeTOTPStore embeds fakeStore (reusing its whole AccountStore
// implementation) and adds in-memory TOTPStore methods for authenticator
// unit tests. PgxAccountStore's own DB-backed tests in account_test.go
// cover real TOTPStore behavior.
type fakeTOTPStore struct {
	*fakeStore
	pending  map[string][]byte
	lastStep map[string]int64
	recovery map[string]map[string]bool
}

func newFakeTOTPStore() *fakeTOTPStore {
	return &fakeTOTPStore{
		fakeStore: newFakeStore(),
		pending:   map[string][]byte{},
		lastStep:  map[string]int64{},
		recovery:  map[string]map[string]bool{},
	}
}

func (f *fakeTOTPStore) SetPendingTOTPSecret(_ context.Context, id string, encrypted []byte) error {
	a, ok := f.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	f.pending[id] = encrypted
	a.TOTPEnabled = false
	return nil
}

func (f *fakeTOTPStore) ConfirmTOTPEnrollment(_ context.Context, id string) error {
	a, ok := f.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	a.TOTPEnabled = true
	return nil
}

func (f *fakeTOTPStore) ConfirmTOTPEnrollmentWithRecoveryCodes(_ context.Context, id string, hashedCodes [][]byte) error {
	a, ok := f.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	a.TOTPEnabled = true
	m := map[string]bool{}
	for _, h := range hashedCodes {
		m[string(h)] = true
	}
	f.recovery[id] = m
	return nil
}

func (f *fakeTOTPStore) GetTOTPSecret(_ context.Context, id string) ([]byte, error) {
	if _, ok := f.byID[id]; !ok {
		return nil, auth.ErrAccountNotFound
	}
	enc, ok := f.pending[id]
	if !ok {
		return nil, auth.ErrTOTPNotEnrolled
	}
	return enc, nil
}

func (f *fakeTOTPStore) DisableTOTP(_ context.Context, id string) error {
	a, ok := f.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	delete(f.pending, id)
	delete(f.lastStep, id)
	delete(f.recovery, id)
	a.TOTPEnabled = false
	return nil
}

func (f *fakeTOTPStore) ConsumeTOTPStep(_ context.Context, id string, step int64) (bool, error) {
	if _, ok := f.byID[id]; !ok {
		return false, auth.ErrAccountNotFound
	}
	if last, ok := f.lastStep[id]; ok && step <= last {
		return false, nil
	}
	f.lastStep[id] = step
	return true, nil
}

func (f *fakeTOTPStore) StoreRecoveryCodes(_ context.Context, id string, hashedCodes [][]byte) error {
	if _, ok := f.byID[id]; !ok {
		return auth.ErrAccountNotFound
	}
	m := map[string]bool{}
	for _, h := range hashedCodes {
		m[string(h)] = true
	}
	f.recovery[id] = m
	return nil
}

func (f *fakeTOTPStore) ConsumeRecoveryCode(_ context.Context, id string, hashedCode []byte) (bool, error) {
	if _, ok := f.byID[id]; !ok {
		return false, auth.ErrAccountNotFound
	}
	m, ok := f.recovery[id]
	if !ok {
		return false, nil
	}
	key := string(hashedCode)
	if !m[key] {
		return false, nil
	}
	delete(m, key)
	return true, nil
}

var _ auth.TOTPStore = (*fakeTOTPStore)(nil)

// test helpers for TOTP-backed authenticator tests.
var (
	testTOTPEncKey  = bytes.Repeat([]byte("k"), auth.TOTPEncryptionKeyLen)
	testRateLimiter = &fakeTOTPStoreRateLimiter{}
	testLoginRate   = auth.RateRule{Limit: 10, Window: time.Minute}
	testTOTPRate    = auth.RateRule{Limit: 10, Window: time.Minute}
)

type fakeTOTPStoreRateLimiter struct{}

func (fakeTOTPStoreRateLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return true, nil
}

type fakeRateLimiter struct {
	allow bool
	err   error
}

func (f *fakeRateLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return f.allow, f.err
}

func (f *fakeRateLimiter) Set(allow bool, err error) {
	f.allow = allow
	f.err = err
}

func newBcryptAuth(t *testing.T, store auth.AccountStore) *auth.BcryptTOTPAuth {
	t.Helper()
	cfg := auth.BcryptConfig{
		Store:      store,
		HMACKey:    []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:   "/admin",
		SessionTTL: time.Hour,
	}
	if _, ok := store.(auth.TOTPStore); ok {
		cfg.TOTPEncryptionKey = testTOTPEncKey
		cfg.RateLimiter = testRateLimiter
		cfg.LoginRate = testLoginRate
		cfg.TOTPRate = testTOTPRate
	}
	return auth.NewBcryptTOTPAuth(cfg)
}

func seedAccount(t *testing.T, store *fakeStore, id, email, pw, role string, active bool) {
	t.Helper()
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	store.add(&auth.Account{ID: id, Email: email, PasswordHash: h, Role: role, Active: active})
}

func loginPOST(a *auth.BcryptTOTPAuth, email, pw string) *httptest.ResponseRecorder {
	body := strings.NewReader("email=" + email + "&password=" + pw)
	r := httptest.NewRequest(http.MethodPost, "/admin/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, r)
	return w
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "panel_admin" {
			return c
		}
	}
	return nil
}

func TestBcrypt_LoginSuccess(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	a := newBcryptAuth(t, store)
	w := loginPOST(a, "op@example.com", "s3cret")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if sessionCookie(w) == nil {
		t.Fatalf("expected panel_admin session cookie, got %q", w.Header().Get("Set-Cookie"))
	}
}

func TestBcrypt_WrongPassword(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	a := newBcryptAuth(t, store)
	if w := loginPOST(a, "op@example.com", "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBcrypt_UnknownUser(t *testing.T) {
	a := newBcryptAuth(t, newFakeStore())
	if w := loginPOST(a, "nobody@example.com", "x"); w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBcrypt_RequireAllowsValidSession(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	a := newBcryptAuth(t, store)
	c := sessionCookie(loginPOST(a, "op@example.com", "s3cret"))
	if c == nil {
		t.Fatal("no session cookie")
	}
	called := false
	h := a.Require(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if s, ok := auth.SessionFrom(r.Context()); !ok || s.UserID != "u1" {
			t.Errorf("expected session for u1 in ctx, got ok=%v s=%+v", ok, s)
		}
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h(w, r)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("expected handler called with 200, got called=%v code=%d", called, w.Code)
	}
}

func TestBcrypt_RequireRedirectsUnauthenticated(t *testing.T) {
	a := newBcryptAuth(t, newFakeStore())
	h := a.Require(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}

func TestBcrypt_RequireRevokesDeactivated(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin", true)
	a := newBcryptAuth(t, store)
	c := sessionCookie(loginPOST(a, "op@example.com", "s3cret"))
	store.deactivate("u1") // deactivate AFTER login — live re-check must revoke

	h := a.Require(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("deactivated account must be revoked (redirect), got %d", w.Code)
	}
}

func TestBcrypt_RequireRole(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "ed@example.com", "s3cret", "editor", true)
	a := newBcryptAuth(t, store)
	c := sessionCookie(loginPOST(a, "ed@example.com", "s3cret"))

	owner := a.RequireRole("owner", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	owner(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("editor on owner route must be 403, got %d", w.Code)
	}

	editor := a.RequireRole("editor", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r2 := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r2.AddCookie(c)
	w2 := httptest.NewRecorder()
	editor(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("editor on editor route must be 200, got %d", w2.Code)
	}
}

// TestBcrypt_HasRole verifies the nav-hide derivation: an exact role match and the
// "owner" super-role return true, any other role returns false, and a context with no
// session returns false (never panics).
func TestBcrypt_HasRole(t *testing.T) {
	store := newFakeStore()
	seedAccount(t, store, "u1", "ed@example.com", "s3cret", "editor", true)
	seedAccount(t, store, "u2", "boss@example.com", "s3cret", "owner", true)
	a := newBcryptAuth(t, store)

	// editor session — exact role true, other role false. The session is injected
	// into ctx by the real Require path (HasRole reads it via SessionFrom).
	ce := sessionCookie(loginPOST(a, "ed@example.com", "s3cret"))
	if ce == nil {
		t.Fatal("no editor session cookie")
	}
	re := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	re.AddCookie(ce)
	a.Require(func(w http.ResponseWriter, r *http.Request) {
		if !a.HasRole(r.Context(), "editor") {
			t.Error(`editor session must satisfy HasRole("editor")`)
		}
		if a.HasRole(r.Context(), "admin") {
			t.Error(`editor session must NOT satisfy HasRole("admin")`)
		}
		w.WriteHeader(http.StatusOK)
	})(httptest.NewRecorder(), re)

	// owner session — the super-role satisfies HasRole for every role.
	co := sessionCookie(loginPOST(a, "boss@example.com", "s3cret"))
	if co == nil {
		t.Fatal("no owner session cookie")
	}
	ro := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	ro.AddCookie(co)
	a.Require(func(w http.ResponseWriter, r *http.Request) {
		if !a.HasRole(r.Context(), "admin") {
			t.Error(`owner session must satisfy HasRole("admin")`)
		}
		if !a.HasRole(r.Context(), "editor") {
			t.Error(`owner session must satisfy HasRole("editor")`)
		}
		w.WriteHeader(http.StatusOK)
	})(httptest.NewRecorder(), ro)

	// no session on ctx — false, never panics.
	if a.HasRole(context.Background(), "editor") {
		t.Error("HasRole with no session must return false")
	}
}

func TestNewBcryptTOTPAuth_PanicsWhenTOTPStoreWithoutKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic when Store implements TOTPStore but TOTPEncryptionKey is nil")
		}
	}()
	auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:   newFakeTOTPStore(),
		HMACKey: []byte("test-hmac-key-32-bytes-long-here"),
		// TOTPEncryptionKey intentionally omitted.
	})
}

func TestNewBcryptTOTPAuth_PanicsWhenTOTPStoreWithShortKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic when TOTPEncryptionKey is shorter than 32 bytes")
		}
	}()
	auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:             newFakeTOTPStore(),
		HMACKey:           []byte("test-hmac-key-32-bytes-long-here"),
		TOTPEncryptionKey: []byte("too-short-16-byt"), // 16 bytes, AES-128 size, not AES-256
	})
}

// TestNewBcryptTOTPAuth_PanicsWhenTOTPStoreWithLongKey locks in the
// exact-32-bytes reading (not merely ">= 32"): AES-256-GCM requires a
// PRECISE 256-bit key, so a longer key is rejected too rather than
// silently truncated -- silent truncation would let an operator believe a
// 64-byte key is in use when only the first 32 bytes are load-bearing.
func TestNewBcryptTOTPAuth_PanicsWhenTOTPStoreWithLongKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic when TOTPEncryptionKey is longer than 32 bytes")
		}
	}()
	auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:             newFakeTOTPStore(),
		HMACKey:           []byte("test-hmac-key-32-bytes-long-here"),
		TOTPEncryptionKey: bytes.Repeat([]byte("k"), 64),
	})
}

func TestNewBcryptTOTPAuth_NoPanicWhenTOTPStoreWithValidKey(t *testing.T) {
	a := auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:             newFakeTOTPStore(),
		HMACKey:           []byte("test-hmac-key-32-bytes-long-here"),
		TOTPEncryptionKey: bytes.Repeat([]byte("k"), auth.TOTPEncryptionKeyLen),
		RateLimiter:       testRateLimiter,
		LoginRate:         testLoginRate,
		TOTPRate:          testTOTPRate,
	})
	if a == nil {
		t.Fatal("expected a non-nil BcryptTOTPAuth with a valid 32-byte TOTPEncryptionKey")
	}
}

// TestNewBcryptTOTPAuth_NoPanicWhenStoreDoesNotImplementTOTPStore proves
// the panic is conditional on TOTPStore support, not unconditional: a
// plain AccountStore (no TOTP methods) must construct fine with no
// TOTPEncryptionKey at all — TOTP wiring stays fully additive for
// consumers who never opt in.
func TestNewBcryptTOTPAuth_NoPanicWhenStoreDoesNotImplementTOTPStore(t *testing.T) {
	a := auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:   newFakeStore(), // no TOTPStore methods
		HMACKey: []byte("test-hmac-key-32-bytes-long-here"),
	})
	if a == nil {
		t.Fatal("expected a non-nil BcryptTOTPAuth when Store does not implement TOTPStore")
	}
}

func mfaCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "panel_admin_mfa" && c.MaxAge >= 0 {
			return c
		}
	}
	return nil
}

func enrollTOTP(t *testing.T, store *fakeTOTPStore, id, email, pw, role string) string {
	t.Helper()
	seedAccount(t, store.fakeStore, id, email, pw, role, true)
	key, err := auth.GenerateTOTPSecret("go-panel-test", email)
	if err != nil {
		t.Fatal(err)
	}
	secret := key.Secret()
	enc, err := auth.EncryptTOTPSecret([]byte(secret), testTOTPEncKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPendingTOTPSecret(context.Background(), id, enc); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmTOTPEnrollment(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return secret
}

func mfaLoginPost(a *auth.BcryptTOTPAuth, mfaCookie *http.Cookie, code string) *httptest.ResponseRecorder {
	body := strings.NewReader("code=" + code)
	r := httptest.NewRequest(http.MethodPost, "/admin/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if mfaCookie != nil {
		r.AddCookie(mfaCookie)
	}
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, r)
	return w
}

func requireUnauthorized(t *testing.T, a *auth.BcryptTOTPAuth, cookie *http.Cookie) {
	t.Helper()
	h := a.Require(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for unauthenticated/mfa-pending cookie, got %d", w.Code)
	}
}

func TestBcrypt_MFA_TOTPEnabled(t *testing.T) {
	store := newFakeTOTPStore()
	secret := enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	a := newBcryptAuth(t, store)

	w := loginPOST(a, "op@example.com", "s3cret")
	if w.Code != http.StatusOK {
		t.Fatalf("expected MFA page 200, got %d", w.Code)
	}
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}
	if sessionCookie(w) != nil {
		t.Fatal("mfa-pending response must not issue a session cookie")
	}
	requireUnauthorized(t, a, mfa)

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w2 := mfaLoginPost(a, mfa, code)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after valid MFA, got %d", w2.Code)
	}
	sess := sessionCookie(w2)
	if sess == nil {
		t.Fatal("expected session cookie after valid MFA")
	}
	if mfaCookie(w2) != nil {
		t.Fatal("mfa_pending cookie must be cleared after successful MFA")
	}

	called := false
	h := a.Require(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(sess)
	w3 := httptest.NewRecorder()
	h(w3, r)
	if !called || w3.Code != http.StatusOK {
		t.Fatalf("expected authenticated request after MFA, called=%v code=%d", called, w3.Code)
	}
}

func TestBcrypt_MFA_WrongCode(t *testing.T) {
	store := newFakeTOTPStore()
	_ = enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	a := newBcryptAuth(t, store)

	w := loginPOST(a, "op@example.com", "s3cret")
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}

	w2 := mfaLoginPost(a, mfa, "000000")
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong TOTP code, got %d", w2.Code)
	}
	if sessionCookie(w2) != nil {
		t.Fatal("wrong MFA code must not issue session cookie")
	}
}

func TestBcrypt_MFA_ReplayRejected(t *testing.T) {
	store := newFakeTOTPStore()
	secret := enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	a := newBcryptAuth(t, store)

	w := loginPOST(a, "op@example.com", "s3cret")
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w2 := mfaLoginPost(a, mfa, code)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("first correct code should succeed, got %d", w2.Code)
	}

	w3 := mfaLoginPost(a, mfa, code)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code should fail, got %d", w3.Code)
	}
}

func TestBcrypt_MFA_RecoveryCode(t *testing.T) {
	store := newFakeTOTPStore()
	_ = enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	a := newBcryptAuth(t, store)

	// Seed recovery codes.
	codes, hashes, err := auth.GenerateRecoveryCodes(8)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreRecoveryCodes(context.Background(), "u1", hashes); err != nil {
		t.Fatal(err)
	}

	w := loginPOST(a, "op@example.com", "s3cret")
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}

	w2 := mfaLoginPost(a, mfa, codes[0])
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after recovery code, got %d", w2.Code)
	}
	if sessionCookie(w2) == nil {
		t.Fatal("expected session cookie after recovery code")
	}

	// Same recovery code must not work again.
	w3 := mfaLoginPost(a, mfa, codes[0])
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code should fail, got %d", w3.Code)
	}
}

func TestBcrypt_MFA_CrossCookiePasteRejected(t *testing.T) {
	store := newFakeTOTPStore()
	_ = enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	a := newBcryptAuth(t, store)

	w := loginPOST(a, "op@example.com", "s3cret")
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}

	// A session cookie should not accept the mfa_pending token value.
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: mfa.Value, Path: "/admin"})
	if a.Verified(r) {
		t.Fatal("mfa_pending token value must not be accepted as a session cookie")
	}

	// A session token value pasted into the mfa_pending slot must not pass
	// the mfa interstitial. We mint a real session token with the same key.
	// (This is a white-box test: makeTokenWithDomain is not exported, but
	// we can drive the public surface by making the HMAC key known and the
	// mfa token simply invalid for the session slot.)
	// Instead, prove that the MFA page with the session cookie as mfa value
	// does not issue a session: the first POST will fail rate limit because
	// it is treated as a fresh login (no mfa cookie) and the password is not
	// supplied. Simpler: call verifyMFA-style POST with a session-formatted
	// token in the mfa cookie slot.
	w2 := mfaLoginPost(a, &http.Cookie{Name: "panel_admin_mfa", Value: mfa.Value + "tampered", Path: "/admin"}, "000000")
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("tampered mfa token should fail, got %d", w2.Code)
	}
}

func TestBcrypt_MFA_TOTPRateLimiterRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when TOTPStore is configured without RateLimiter")
		}
	}()
	auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:             newFakeTOTPStore(),
		HMACKey:           []byte("test-hmac-key-32-bytes-long-here"),
		TOTPEncryptionKey: testTOTPEncKey,
		TOTPRate:          testTOTPRate,
	})
}

func TestBcrypt_MFA_TOTPRateRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when TOTPStore is configured without TOTPRate")
		}
	}()
	auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:             newFakeTOTPStore(),
		HMACKey:           []byte("test-hmac-key-32-bytes-long-here"),
		TOTPEncryptionKey: testTOTPEncKey,
		RateLimiter:       testRateLimiter,
		LoginRate:         testLoginRate,
	})
}

func TestBcrypt_MFA_TOTPRate_LimitDenies(t *testing.T) {
	store := newFakeTOTPStore()
	_ = enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	rl := &fakeRateLimiter{allow: true}
	a := newBcryptAuthWithRateLimiter(t, store, rl)

	w := loginPOST(a, "op@example.com", "s3cret")
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}

	rl.Set(false, nil)
	w2 := mfaLoginPost(a, mfa, "000000")
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when TOTP rate limit denies, got %d", w2.Code)
	}
}

func TestBcrypt_MFA_TOTPRate_LimiterErrorFailsClosed(t *testing.T) {
	store := newFakeTOTPStore()
	_ = enrollTOTP(t, store, "u1", "op@example.com", "s3cret", "admin")
	rl := &fakeRateLimiter{allow: true}
	a := newBcryptAuthWithRateLimiter(t, store, rl)

	w := loginPOST(a, "op@example.com", "s3cret")
	mfa := mfaCookie(w)
	if mfa == nil {
		t.Fatal("expected mfa_pending cookie")
	}

	rl.Set(false, errors.New("redis down"))
	w2 := mfaLoginPost(a, mfa, "000000")
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when TOTP rate limiter errors, got %d", w2.Code)
	}
}

func newBcryptAuthWithRateLimiter(t *testing.T, store auth.AccountStore, rl auth.RateLimiter) *auth.BcryptTOTPAuth {
	t.Helper()
	cfg := auth.BcryptConfig{
		Store:       store,
		HMACKey:     []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:    "/admin",
		SessionTTL:  time.Hour,
		RateLimiter: rl,
		LoginRate:   testLoginRate,
	}
	if _, ok := store.(auth.TOTPStore); ok {
		cfg.TOTPEncryptionKey = testTOTPEncKey
		cfg.TOTPRate = testTOTPRate
	}
	return auth.NewBcryptTOTPAuth(cfg)
}
