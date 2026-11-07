package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

const (
	oldPW = "old-password-123"
	newPW = "brand-new-password-456"
)

func pwFixture(t *testing.T) (*fakeStore, *auth.Account) {
	t.Helper()
	store := newFakeStore()
	seedAccount(t, store, "u1", "op@example.com", oldPW, "admin", true)
	return store, store.byID["u1"]
}

// F4 at the auth layer: a wrong current password changes nothing.
//
// Falsification: in auth/password.go ChangePassword, delete the
// `if !VerifyPassword(current, stored.PasswordHash)` check -> RED on the hash
// comparison below.
func TestChangePassword_WrongCurrentChangesNothing(t *testing.T) {
	store, acct := pwFixture(t)
	before := store.byID["u1"].PasswordHash
	err := auth.ChangePassword(context.Background(), store, acct, "not-the-password", newPW)
	if !errors.Is(err, auth.ErrReauthFailed) {
		t.Fatalf("err = %v, want ErrReauthFailed", err)
	}
	if got := store.byID["u1"].PasswordHash; got != before {
		t.Error("a wrong current password changed the stored hash")
	}
	if store.byID["u1"].PasswordChangedAt != nil {
		t.Error("a wrong current password stamped the credential epoch")
	}
}

func TestChangePassword_Succeeds(t *testing.T) {
	store, acct := pwFixture(t)
	if err := auth.ChangePassword(context.Background(), store, acct, oldPW, newPW); err != nil {
		t.Fatal(err)
	}
	got := store.byID["u1"]
	if !auth.VerifyPassword(newPW, got.PasswordHash) || auth.VerifyPassword(oldPW, got.PasswordHash) {
		t.Error("stored hash does not verify the new password only")
	}
	if got.PasswordChangedAt == nil {
		t.Error("the credential epoch was not stamped")
	}
}

func TestChangePassword_Policy(t *testing.T) {
	store, acct := pwFixture(t)
	before := store.byID["u1"].PasswordHash
	cases := []struct {
		name, next string
		want       error
	}{
		{"short", "short-9ch", auth.ErrPasswordTooShort},
		{"short in runes not bytes", strings.Repeat("я", auth.MinPasswordLength-1), auth.ErrPasswordTooShort},
		{"long", strings.Repeat("a", auth.MaxPasswordBytes+1), auth.ErrPasswordTooLong},
		{"unchanged", oldPW, auth.ErrPasswordUnchanged},
	}
	for _, c := range cases {
		err := auth.ChangePassword(context.Background(), store, acct, oldPW, c.next)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if err != nil && (strings.Contains(err.Error(), c.next) || strings.Contains(err.Error(), oldPW)) {
			t.Errorf("%s: error text contains a password: %q", c.name, err)
		}
	}
	if store.byID["u1"].PasswordHash != before {
		t.Error("a rejected password changed the stored hash")
	}
	if err := auth.ChangePassword(context.Background(), store, acct, oldPW, strings.Repeat("я", auth.MinPasswordLength)); err != nil {
		t.Errorf("exactly MinPasswordLength runes must pass: %v", err)
	}
}

func TestChangePassword_UnknownAccountLooksLikeAWrongPassword(t *testing.T) {
	store, _ := pwFixture(t)
	err := auth.ChangePassword(context.Background(), store, &auth.Account{ID: "ghost", Email: "ghost@example.com"}, oldPW, newPW)
	if !errors.Is(err, auth.ErrReauthFailed) {
		t.Errorf("err = %v, want ErrReauthFailed", err)
	}
}

func requireStatus(a *auth.BcryptTOTPAuth, c *http.Cookie) int {
	h := a.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// A password change revokes every earlier session (the credential epoch);
// RefreshSession keeps the caller's browser signed in without un-revoking the
// others.
func TestRefreshSession_KeepsTheCallerAndRevokesTheRest(t *testing.T) {
	store, acct := pwFixture(t)
	a := newBcryptAuth(t, store)
	here := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	other := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	time.Sleep(5 * time.Millisecond)

	if err := auth.ChangePassword(context.Background(), store, acct, oldPW, newPW); err != nil {
		t.Fatal(err)
	}
	if requireStatus(a, here) == http.StatusNoContent {
		t.Fatal("precondition: the password change must revoke the caller's old token")
	}
	rec := httptest.NewRecorder()
	if err := a.RefreshSession(rec, httptest.NewRequest(http.MethodPost, "/admin/x", nil), "u1"); err != nil {
		t.Fatal(err)
	}
	fresh := sessionCookie(rec)
	if fresh == nil || requireStatus(a, fresh) != http.StatusNoContent {
		t.Error("the refreshed session is not valid")
	}
	if requireStatus(a, other) == http.StatusNoContent {
		t.Error("another device's session survived the password change")
	}
}

// A database clock a little ahead of this process must not revoke the token
// being issued.
func TestRefreshSession_SurvivesAFastDatabaseClock(t *testing.T) {
	store, _ := pwFixture(t)
	a := newBcryptAuth(t, store)
	ahead := time.Now().Add(3 * time.Second)
	cp := *store.byID["u1"]
	cp.PasswordChangedAt = &ahead
	store.byID["u1"] = &cp
	rec := httptest.NewRecorder()
	if err := a.RefreshSession(rec, httptest.NewRequest(http.MethodPost, "/admin/x", nil), "u1"); err != nil {
		t.Fatal(err)
	}
	if c := sessionCookie(rec); c == nil || requireStatus(a, c) != http.StatusNoContent {
		t.Error("a session issued after a stamp from a fast clock was revoked")
	}
}

func TestRefreshSession_RefusesAnInactiveAccount(t *testing.T) {
	store, _ := pwFixture(t)
	a := newBcryptAuth(t, store)
	store.deactivate("u1")
	rec := httptest.NewRecorder()
	if err := a.RefreshSession(rec, httptest.NewRequest(http.MethodPost, "/admin/x", nil), "u1"); !errors.Is(err, auth.ErrAccountNotFound) {
		t.Errorf("err = %v", err)
	}
	if sessionCookie(rec) != nil {
		t.Error("a cookie was set for an inactive account")
	}
}
