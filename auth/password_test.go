package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

const (
	oldPW = "old-password-123"
	newPW = "brand-new-password-456"
)

// epochStore is the in-memory fakeStore with the EpochStore contract: the
// conditional stamp is atomic under mu, like the database's single UPDATE.
type epochStore struct {
	*fakeStore
	mu           sync.Mutex
	beforeUpdate func() // test hook: runs after the caller verified, before the write
	getByEmail   func(email string) (*auth.Account, error)
}

func (s *epochStore) GetByEmail(ctx context.Context, email string) (*auth.Account, error) {
	if s.getByEmail != nil {
		return s.getByEmail(email)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.GetByEmail(ctx, email)
}

func (s *epochStore) GetByID(ctx context.Context, id string) (*auth.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.GetByID(ctx, id)
}

func (s *epochStore) stamp(id, hash string, iat int64) (time.Time, error) {
	if s.beforeUpdate != nil {
		s.beforeUpdate()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok || (a.PasswordChangedAt != nil && a.PasswordChangedAt.After(time.UnixMilli(iat))) {
		return time.Time{}, auth.ErrSessionStale
	}
	now := time.Now()
	cp := *a
	if hash != "" {
		cp.PasswordHash = hash
	}
	cp.PasswordChangedAt = &now
	s.byID[id], s.byEmail[cp.Email] = &cp, &cp
	return now, nil
}

func (s *epochStore) UpdatePasswordHashIfCurrent(_ context.Context, id, hash string, iat int64) (time.Time, error) {
	return s.stamp(id, hash, iat)
}

func (s *epochStore) RevokeSessionsIfCurrent(_ context.Context, id string, iat int64) (time.Time, error) {
	return s.stamp(id, "", iat)
}

func (s *epochStore) hash(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id].PasswordHash
}

func (s *epochStore) epoch(id string) *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id].PasswordChangedAt
}

func pwFixture(t *testing.T) (*epochStore, *auth.Account) {
	t.Helper()
	fs := newFakeStore()
	seedAccount(t, fs, "u1", "op@example.com", oldPW, "admin", true)
	return &epochStore{fakeStore: fs}, fs.byID["u1"]
}

var nowIat = func() int64 { return time.Now().UnixMilli() }

// F4 at the auth layer: a wrong current password changes nothing.
//
// Falsification: in auth/password.go loadVerified delete the
// `if !VerifyPassword(current, stored.PasswordHash)` check -> RED.
func TestChangePassword_WrongCurrentChangesNothing(t *testing.T) {
	store, acct := pwFixture(t)
	before := store.hash("u1")
	_, err := auth.ChangePassword(context.Background(), store, acct, nowIat(), "not-the-password", newPW)
	if !errors.Is(err, auth.ErrReauthFailed) {
		t.Fatalf("err = %v, want ErrReauthFailed", err)
	}
	if store.hash("u1") != before || store.epoch("u1") != nil {
		t.Error("a wrong current password changed the hash or stamped the epoch")
	}
}

func TestChangePassword_Succeeds(t *testing.T) {
	store, acct := pwFixture(t)
	epoch, err := auth.ChangePassword(context.Background(), store, acct, nowIat(), oldPW, newPW)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword(newPW, store.hash("u1")) || auth.VerifyPassword(oldPW, store.hash("u1")) {
		t.Error("stored hash does not verify the new password only")
	}
	if got := store.epoch("u1"); got == nil || !got.Equal(epoch) {
		t.Errorf("returned epoch %v is not the stamp the store wrote (%v)", epoch, got)
	}
}

func TestChangePassword_Policy(t *testing.T) {
	store, acct := pwFixture(t)
	before := store.hash("u1")
	for _, c := range []struct {
		name, next string
		want       error
	}{
		{"short", "short-9ch", auth.ErrPasswordTooShort},
		{"short in runes not bytes", strings.Repeat("я", auth.MinPasswordLength-1), auth.ErrPasswordTooShort},
		{"long", strings.Repeat("a", auth.MaxPasswordBytes+1), auth.ErrPasswordTooLong},
		{"unchanged", oldPW, auth.ErrPasswordUnchanged},
	} {
		_, err := auth.ChangePassword(context.Background(), store, acct, nowIat(), oldPW, c.next)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if err != nil && (strings.Contains(err.Error(), c.next) || strings.Contains(err.Error(), oldPW)) {
			t.Errorf("%s: error text contains a password: %q", c.name, err)
		}
	}
	if store.hash("u1") != before {
		t.Error("a rejected password changed the stored hash")
	}
}

// SEC-CR-006: the lookup must land on the session's account; a missing account
// and a (nil, nil) store answer look like a wrong password; a store outage is
// an error of its own, never "wrong password".
func TestChangePassword_LookupEdgeCases(t *testing.T) {
	store, acct := pwFixture(t)
	ctx := context.Background()

	store.getByEmail = func(string) (*auth.Account, error) { return nil, auth.ErrAccountNotFound }
	if _, err := auth.ChangePassword(ctx, store, acct, nowIat(), oldPW, newPW); !errors.Is(err, auth.ErrReauthFailed) {
		t.Errorf("unknown account: err = %v, want ErrReauthFailed", err)
	}
	store.getByEmail = func(string) (*auth.Account, error) { return nil, nil } //nolint:nilnil // the contract violation under test
	if _, err := auth.ChangePassword(ctx, store, acct, nowIat(), oldPW, newPW); !errors.Is(err, auth.ErrReauthFailed) {
		t.Errorf("(nil, nil) from the store: err = %v, want ErrReauthFailed (and no panic)", err)
	}
	other := *acct
	other.ID = "someone-else"
	store.getByEmail = func(string) (*auth.Account, error) { return &other, nil }
	if _, err := auth.ChangePassword(ctx, store, acct, nowIat(), oldPW, newPW); !errors.Is(err, auth.ErrReauthFailed) {
		t.Errorf("row of another account: err = %v, want ErrReauthFailed", err)
	}
	outage := errors.New("db down")
	store.getByEmail = func(string) (*auth.Account, error) { return nil, outage }
	_, err := auth.ChangePassword(ctx, store, acct, nowIat(), oldPW, newPW)
	if errors.Is(err, auth.ErrReauthFailed) || !errors.Is(err, outage) {
		t.Errorf("store outage: err = %v, want the outage (not ErrReauthFailed)", err)
	}
}

// SEC-CR-004: two concurrent changes from the same session; exactly one wins.
//
// Falsification: in auth/account_pgx.go epochGuard drop the password_changed_at
// condition (pgx test below), or here: make epochStore.stamp ignore the stamp
// check -> RED.
func TestChangePassword_ConcurrentChangesExactlyOneWins(t *testing.T) {
	store, acct := pwFixture(t)
	iat := nowIat()
	time.Sleep(3 * time.Millisecond)
	var arrived sync.WaitGroup
	arrived.Add(2)
	store.beforeUpdate = func() { arrived.Done(); arrived.Wait() } // both verified, both about to write

	results := make([]error, 2)
	var wg sync.WaitGroup
	for i, next := range []string{"first-new-password-1", "second-new-password-2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = auth.ChangePassword(context.Background(), store, acct, iat, oldPW, next)
		}()
	}
	wg.Wait()
	wins, stale := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, auth.ErrSessionStale):
			stale++
		default:
			t.Errorf("unexpected error %v", err)
		}
	}
	if wins != 1 || stale != 1 {
		t.Errorf("wins=%d stale=%d, want exactly one of each", wins, stale)
	}
}

func TestRevokeOtherSessions_NeedsThePassword(t *testing.T) {
	store, acct := pwFixture(t)
	if _, err := auth.RevokeOtherSessions(context.Background(), store, acct, nowIat(), "wrong"); !errors.Is(err, auth.ErrReauthFailed) {
		t.Fatalf("err = %v, want ErrReauthFailed", err)
	}
	if store.epoch("u1") != nil {
		t.Error("a wrong password stamped the epoch")
	}
	if _, err := auth.RevokeOtherSessions(context.Background(), store, acct, nowIat(), oldPW); err != nil {
		t.Fatal(err)
	}
	if store.epoch("u1") == nil {
		t.Error("the revoke did not stamp the epoch")
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

func requestWith(c *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/x", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

// A change revokes every earlier session; RefreshSession keeps the caller's
// browser signed in from the epoch the store returned, without un-revoking the
// others.
func TestRefreshSession_KeepsTheCallerAndRevokesTheRest(t *testing.T) {
	store, acct := pwFixture(t)
	a := newBcryptAuth(t, store)
	here := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	other := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	time.Sleep(5 * time.Millisecond)

	epoch, err := auth.ChangePassword(context.Background(), store, acct, nowIat(), oldPW, newPW)
	if err != nil {
		t.Fatal(err)
	}
	if requireStatus(a, here) == http.StatusNoContent {
		t.Fatal("precondition: the change must revoke the caller's old token")
	}
	rec := httptest.NewRecorder()
	if err := a.RefreshSession(rec, requestWith(here), epoch); err != nil {
		t.Fatal(err)
	}
	if fresh := sessionCookie(rec); fresh == nil || requireStatus(a, fresh) != http.StatusNoContent {
		t.Error("the refreshed session is not valid")
	}
	if requireStatus(a, other) == http.StatusNoContent {
		t.Error("another device's session survived the password change")
	}
}

// SEC-CR-002: identity comes from the request's own session. No valid cookie,
// no session minted.
//
// Falsification: in auth/bcrypt_auth.go RefreshSession, skip the parseToken
// check (trust a cookie's decoded payload) -> RED on "tampered".
func TestRefreshSession_MintsNothingWithoutAValidSession(t *testing.T) {
	store, _ := pwFixture(t)
	a := newBcryptAuth(t, store)
	good := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	tampered := *good
	tampered.Value = good.Value[:len(good.Value)-2] + "00"
	garbage := &http.Cookie{Name: good.Name, Value: "not.a-token"}
	for name, c := range map[string]*http.Cookie{"no cookie": nil, "garbage": garbage, "tampered MAC": &tampered} {
		rec := httptest.NewRecorder()
		err := a.RefreshSession(rec, requestWith(c), time.Now())
		if !errors.Is(err, auth.ErrNoSession) {
			t.Errorf("%s: err = %v, want ErrNoSession", name, err)
		}
		if sessionCookie(rec) != nil {
			t.Errorf("%s: a session cookie was minted", name)
		}
	}
	store.deactivate("u1")
	rec := httptest.NewRecorder()
	if err := a.RefreshSession(rec, requestWith(good), time.Now()); !errors.Is(err, auth.ErrNoSession) || sessionCookie(rec) != nil {
		t.Errorf("inactive account: err = %v, minted = %v", err, sessionCookie(rec) != nil)
	}
}

// The new token is floored above the epoch the DATABASE stamped, even when
// that clock runs ahead of this process.
func TestRefreshSession_SurvivesAFastDatabaseClock(t *testing.T) {
	store, _ := pwFixture(t)
	a := newBcryptAuth(t, store)
	here := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	ahead := time.Now().Add(3 * time.Second)
	cp := *store.byID["u1"]
	cp.PasswordChangedAt = &ahead
	store.byID["u1"] = &cp
	rec := httptest.NewRecorder()
	if err := a.RefreshSession(rec, requestWith(here), ahead); err != nil {
		t.Fatal(err)
	}
	if c := sessionCookie(rec); c == nil || requireStatus(a, c) != http.StatusNoContent {
		t.Error("a session issued after a stamp from a fast clock was revoked")
	}
}

func TestSessionCarriesItsIssueTime(t *testing.T) {
	store, _ := pwFixture(t)
	a := newBcryptAuth(t, store)
	before := time.Now().UnixMilli()
	c := sessionCookie(loginPOST(a, "op@example.com", oldPW))
	var got *auth.Session
	h := a.Require(func(_ http.ResponseWriter, r *http.Request) { got, _ = auth.SessionFrom(r.Context()) })
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.AddCookie(c)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got == nil || got.IssuedAtMs < before || got.IssuedAtMs > time.Now().UnixMilli() {
		t.Errorf("Session.IssuedAtMs = %+v, want the login time", got)
	}
}
