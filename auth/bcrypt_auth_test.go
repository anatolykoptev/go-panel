package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

// fakeStore is an in-memory AccountStore for authenticator unit tests.
type fakeStore struct {
	byEmail map[string]*auth.Account
	byID    map[string]*auth.Account
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

func (f *fakeStore) GetByEmail(_ context.Context, email string) (*auth.Account, error) {
	a, ok := f.byEmail[email]
	if !ok || !a.Active || a.PasswordHash == "" {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeStore) GetByID(_ context.Context, id string) (*auth.Account, error) {
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

func newBcryptAuth(t *testing.T, store auth.AccountStore) *auth.BcryptTOTPAuth {
	t.Helper()
	return auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:      store,
		HMACKey:    []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:   "/admin",
		SessionTTL: time.Hour,
	})
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
