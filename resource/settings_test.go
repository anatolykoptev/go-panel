package resource_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

// settingsStore is the multi-user test store with the auth.EpochStore contract:
// the conditional stamp is atomic under mu, like the database's single UPDATE.
type settingsStore struct {
	*totpTestStore
	mu sync.Mutex
	// beforeStamp runs inside an epoch write, after the caller passed Require, CSRF
	// and the password check and before the conditional write: the window an
	// in-flight request sits in.
	beforeStamp func()
	// failUpdate makes the write return a store error (an outage).
	failUpdate bool
	// breakAfterStamp makes GetByID fail once a stamp was written, so the session
	// cannot be re-issued (the change still happened).
	breakAfterStamp bool
	stamped         bool
}

func (s *settingsStore) GetByID(ctx context.Context, id string) (*auth.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.breakAfterStamp && s.stamped {
		return nil, context.DeadlineExceeded
	}
	return s.totpTestStore.GetByID(ctx, id)
}

func (s *settingsStore) stamp(id, hash string, iat int64) (time.Time, error) {
	if s.beforeStamp != nil {
		s.beforeStamp()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failUpdate {
		return time.Time{}, context.DeadlineExceeded
	}
	a, ok := s.byID[id]
	if !ok || (a.PasswordChangedAt != nil && a.PasswordChangedAt.After(time.UnixMilli(iat))) {
		return time.Time{}, auth.ErrSessionStale
	}
	now := time.Now()
	if hash != "" {
		a.PasswordHash = hash
	}
	a.PasswordChangedAt = &now
	s.stamped = true
	return now, nil
}

func (s *settingsStore) UpdatePasswordHashIfCurrent(_ context.Context, id, hash string, iat int64) (time.Time, error) {
	return s.stamp(id, hash, iat)
}

func (s *settingsStore) RevokeSessionsIfCurrent(_ context.Context, id string, iat int64) (time.Time, error) {
	return s.stamp(id, "", iat)
}

func (s *settingsStore) hash(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id].PasswordHash
}

const (
	owner0PW = "owner-password-0"
	newPW    = "a-much-better-password-9"
)

type settingsEnv struct {
	actionRuns atomic.Int32
	p          *resource.Panel
	h          http.Handler
	a          *auth.BcryptTOTPAuth
	store      *settingsStore
}

type settingsOpts struct {
	skin    string
	skins   []string
	rate    auth.RateRule
	noEpoch bool // hand MountSettings a store without auth.EpochStore
	totp    bool
}

func newSettingsEnv(t *testing.T, o settingsOpts) *settingsEnv {
	t.Helper()
	base := newTOTPTestStore()
	i18nSeed(t, base.testAccountStore, "own", "owner@example.com", owner0PW, auth.RoleOwner)
	i18nSeed(t, base.testAccountStore, "mgr", "manager@example.com", "manager-password-1", "manager")
	base.byID["own"].Name = "Olga Owner"
	store := &settingsStore{totpTestStore: base}
	a := i18nBcryptAuth(store, nil, auth.RateRule{})
	p := resource.New(resource.Config{
		Title: "Settings Panel", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey,
		Skin: o.skin, Skins: o.skins, Strings: resource.RussianStrings(),
	})
	if o.totp {
		resource.MountTOTPEnrollment(p, resource.TOTPEnrollmentConfig{Store: store, TOTPEncryptionKey: totpTestEncKey, Issuer: "T", PathPrefix: "security/totp"})
	}
	cfg := resource.SettingsConfig{
		PasswordRate: o.rate,
		Pages: []resource.SettingsPage{{
			ID: "collection", Title: "Сбор данных", RequiredRole: auth.RoleOwner,
			Body: templ.Raw(`<p id="collection-body">owner only</p>`),
		}},
	}
	cfg.Store = store
	if o.noEpoch {
		cfg.Store = base
	}
	p.MountSettings(cfg)
	env := &settingsEnv{p: p, a: a, store: store}
	p.MountSettingsAction("collection", resource.ActionSpec{Path: "toggle", Handler: func(w http.ResponseWriter, _ *http.Request) {
		env.actionRuns.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}})
	env.h = p.Handler()
	return env
}

func (e *settingsEnv) do(t *testing.T, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequestWithContext(context.Background(), method, path, nil)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *settingsEnv) login(t *testing.T, email, pw string) *http.Cookie {
	t.Helper()
	return bcryptLogin(t, e.a, email, pw)
}

func (e *settingsEnv) token(sess *http.Cookie) string {
	return csrf.Issue(i18nCSRFKey, sess.Value, csrf.DefaultTTL)
}

func (e *settingsEnv) changePW(t *testing.T, sess *http.Cookie, form url.Values, extra ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if form.Get("_csrf") == "" && form.Get("noToken") == "" {
		form.Set("_csrf", e.token(sess))
	}
	form.Del("noToken")
	return e.do(t, http.MethodPost, "/admin/settings/security/password/", form, append([]*http.Cookie{sess}, extra...)...)
}

func pwForm(cur, next, repeat string) url.Values {
	return url.Values{"current_password": {cur}, "new_password": {next}, "repeat_password": {repeat}}
}

// F2: a consumer that never calls MountSettings sees no route and no link.
//
// Falsification: in resource/page.go finalize, call p.MountSettings when
// p.settings is nil -> RED (route answers, link rendered).
func TestSettings_NotMountedMeansNoRouteNoLink(t *testing.T) {
	base := newTOTPTestStore()
	i18nSeed(t, base.testAccountStore, "own", "owner@example.com", owner0PW, auth.RoleOwner)
	a := i18nBcryptAuth(base, nil, auth.RateRule{})
	p := resource.New(resource.Config{Title: "P", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey})
	p.SetProfile(shell.ProfileConfig{Name: "Olga"})
	h := p.Handler()
	sess := bcryptLogin(t, a, "owner@example.com", owner0PW)
	for _, path := range []string{"/admin/settings", "/admin/settings/", "/admin/settings/profile/", "/admin/settings/appearance/"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		req.AddCookie(sess)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d on a panel without MountSettings, want 404", path, rec.Code)
		}
	}
	if body := renderSkinned(t, p, sess); strings.Contains(body, "/settings") {
		t.Error("a Settings link renders without MountSettings")
	}
}

func TestSettings_RedirectsToTheFirstPage(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	// F8: /settings -> 302 /settings/profile/.
	//
	// Falsification: in resource/settings.go redirectToFirst, index the last
	// page instead of the first -> RED.
	rec := e.do(t, http.MethodGet, "/admin/settings", nil, sess)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/settings/profile/" {
		t.Errorf("GET /admin/settings = %d -> %q, want 302 -> /admin/settings/profile/", rec.Code, rec.Header().Get("Location"))
	}
}

// F7: a role-gated consumer page is absent from the sub-nav and 403 for a
// session without the role.
//
// Falsification: in resource/settings.go MountSettings, drop RequiredRole from
// the MountPage call of each page -> RED (the manager gets 404 instead of 403).
func TestSettings_RoleGatedPage(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	mgr := e.login(t, "manager@example.com", "manager-password-1")
	own := e.login(t, "owner@example.com", owner0PW)

	rec := e.do(t, http.MethodGet, "/admin/settings/collection/", nil, mgr)
	if rec.Code != http.StatusForbidden {
		t.Errorf("manager GET collection = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "collection-body") {
		t.Error("the owner-only body reached a manager")
	}
	if nav := e.do(t, http.MethodGet, "/admin/settings/profile/", nil, mgr).Body.String(); strings.Contains(nav, "/admin/settings/collection/") {
		t.Error("a manager's sub-nav lists the owner-only page")
	}
	rec = e.do(t, http.MethodGet, "/admin/settings/collection/", nil, own)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "collection-body") {
		t.Errorf("owner GET collection = %d", rec.Code)
	}
	if nav := rec.Body.String(); !strings.Contains(nav, `href="/admin/settings/collection/" aria-current="page"`) || !strings.Contains(nav, "Рабочее пространство") {
		t.Error("the owner's sub-nav lacks the workspace group or the active marker")
	}
}

func TestSettings_ProfilePage(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	body := e.do(t, http.MethodGet, "/admin/settings/profile/", nil, sess).Body.String()
	for _, want := range []string{"Вы вошли как Olga Owner", "owner@example.com", "owner", "Выйти", "Личные", "Безопасность", "Оформление", `action="/admin/logout"`} {
		if !strings.Contains(body, want) {
			t.Errorf("profile page lacks %q", want)
		}
	}
	if !strings.Contains(body, `href="/admin/settings/"`) {
		t.Error("the sidebar Settings item does not point at the area")
	}
}

// A single-user panel (no per-operator identity) gets the Appearance page and
// consumer pages only: Profile and Security need an account.
func TestSettings_SingleUserPanelHasNoAccountPages(t *testing.T) {
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw", HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/admin"})
	p := resource.New(resource.Config{Title: "P", BasePath: "/admin", Auth: a, Skin: shell.SkinIris, Skins: []string{shell.SkinGraphite}})
	p.MountSettings(resource.SettingsConfig{})
	h := p.Handler()
	sess := i18nHMACCookie(t, a)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		req.AddCookie(sess)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("/admin/settings"); rec.Header().Get("Location") != "/admin/settings/appearance/" {
		t.Errorf("redirect -> %q, want the first page that exists", rec.Header().Get("Location"))
	}
	for _, gone := range []string{"profile", "security"} {
		if rec := get("/admin/settings/" + gone + "/"); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d on a panel with no accounts, want 404", gone, rec.Code)
		}
	}
	body := get("/admin/settings/appearance/").Body.String()
	if !strings.Contains(body, `href="/admin/settings/"`) {
		t.Error("the single-user sidebar lacks the Settings item")
	}
}

// F4 over HTTP: a wrong current password changes nothing, and neither does a
// request that skips CSRF (F5).
//
// Falsification (F4): in auth/password.go ChangePassword skip the current-
// password check -> RED on the hash assertion.
func TestSettings_WrongCurrentPasswordChangesNothing(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	before := e.store.hash("own")
	rec := e.changePW(t, sess, pwForm("definitely-wrong", newPW, newPW))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Текущий пароль неверный") {
		t.Errorf("wrong current password: %d, body has message: %v", rec.Code, strings.Contains(rec.Body.String(), "Текущий пароль неверный"))
	}
	if e.store.hash("own") != before {
		t.Fatal("a wrong current password changed the stored hash")
	}
}

// F5: the password POST requires a valid CSRF token.
//
// Falsification: in resource/settings.go changePassword, delete the
// verifyCSRFToken call -> RED (the change goes through without a token).
func TestSettings_PasswordPostRequiresCSRF(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	before := e.store.hash("own")
	for name, form := range map[string]url.Values{
		"no token":     func() url.Values { f := pwForm(owner0PW, newPW, newPW); f.Set("noToken", "1"); return f }(),
		"forged token": func() url.Values { f := pwForm(owner0PW, newPW, newPW); f.Set("_csrf", "forged"); return f }(),
		"other sessions": func() url.Values {
			f := pwForm(owner0PW, newPW, newPW)
			f.Set("_csrf", csrf.Issue(i18nCSRFKey, "someone-else", csrf.DefaultTTL))
			return f
		}(),
	} {
		rec := e.changePW(t, sess, form)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, rec.Code)
		}
		if e.store.hash("own") != before {
			t.Fatalf("%s: the password changed without a valid CSRF token", name)
		}
	}
}

func TestSettings_PasswordPolicyAndMismatch(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	before := e.store.hash("own")
	for name, c := range map[string]struct {
		form url.Values
		want string
	}{
		"too short": {pwForm(owner0PW, "short", "short"), "не короче 10 символов"},
		"too long":  {pwForm(owner0PW, strings.Repeat("x", 73), strings.Repeat("x", 73)), "слишком длинный"},
		"mismatch":  {pwForm(owner0PW, newPW, newPW+"!"), "не совпадают"},
		"unchanged": {pwForm(owner0PW, owner0PW, owner0PW), "отличаться от текущего"},
	} {
		rec := e.changePW(t, sess, c.form)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: status %d, message %q present: %v", name, rec.Code, c.want, strings.Contains(rec.Body.String(), c.want))
		}
	}
	if e.store.hash("own") != before {
		t.Fatal("a rejected password changed the stored hash")
	}
}

func TestSettings_PasswordAttemptsAreThrottled(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{rate: auth.RateRule{Limit: 3, Window: time.Hour}})
	sess := e.login(t, "owner@example.com", owner0PW)
	for i := range 3 {
		if rec := e.changePW(t, sess, pwForm("wrong-guess-"+string(rune('a'+i)), newPW, newPW)); rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d: status %d", i, rec.Code)
		}
	}
	before := e.store.hash("own")
	rec := e.changePW(t, sess, pwForm(owner0PW, newPW, newPW)) // the RIGHT password, over quota
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("over quota: status %d, Retry-After %q, want 429 with a hint", rec.Code, rec.Header().Get("Retry-After"))
	}
	if e.store.hash("own") != before {
		t.Error("a throttled attempt changed the password")
	}
}

type failingLimiter struct{}

func (failingLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return true, context.DeadlineExceeded
}

// A limiter outage denies, like the login throttle does.
func TestSettings_LimiterErrorFailsClosed(t *testing.T) {
	base := newTOTPTestStore()
	i18nSeed(t, base.testAccountStore, "own", "owner@example.com", owner0PW, auth.RoleOwner)
	store := &settingsStore{totpTestStore: base}
	a := i18nBcryptAuth(store, nil, auth.RateRule{})
	p := resource.New(resource.Config{Title: "P", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey})
	p.MountSettings(resource.SettingsConfig{Store: store, RateLimiter: failingLimiter{}})
	e := &settingsEnv{p: p, h: p.Handler(), a: a, store: store}
	sess := e.login(t, "owner@example.com", owner0PW)
	before := store.hash("own")
	if rec := e.changePW(t, sess, pwForm(owner0PW, newPW, newPW)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("limiter error: status %d, want 429 (fail closed)", rec.Code)
	}
	if store.hash("own") != before {
		t.Error("the password changed while the limiter was down")
	}
}

// The whole journey: change the password, stay signed in here, be signed out
// everywhere else, and sign in with the new password only.
func TestSettings_ChangePasswordJourney(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	here := e.login(t, "owner@example.com", owner0PW)
	elsewhere := e.login(t, "owner@example.com", owner0PW)
	time.Sleep(5 * time.Millisecond)

	rec := e.changePW(t, here, pwForm(owner0PW, newPW, newPW))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/settings/security/?changed=1" {
		t.Fatalf("change: %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	var fresh *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "panel_admin" {
			fresh = c
		}
	}
	if fresh == nil {
		t.Fatal("no refreshed session cookie: the operator would be signed out of the browser they changed it in")
	}
	if got := e.do(t, http.MethodGet, "/admin/settings/security/?changed=1", nil, fresh); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "Пароль изменён") {
		t.Errorf("confirmation page: %d", got.Code)
	}
	if got := e.do(t, http.MethodGet, "/admin/settings/profile/", nil, elsewhere); got.Code == http.StatusOK {
		t.Error("another device stayed signed in after the password change")
	}
	if got := e.do(t, http.MethodGet, "/admin/settings/profile/", nil, here); got.Code == http.StatusOK {
		t.Error("the old token of this browser is still valid (it must be replaced, not revived)")
	}
	_ = bcryptLogin(t, e.a, "owner@example.com", newPW)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", strings.NewReader("email=owner@example.com&password="+owner0PW))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	e.a.LoginHandler().ServeHTTP(w, req)
	if w.Code == http.StatusSeeOther {
		t.Error("the old password still signs in")
	}
}

func (e *settingsEnv) signOutOthers(t *testing.T, sess *http.Cookie, pw string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodPost, "/admin/settings/security/sessions/", url.Values{"_csrf": {e.token(sess)}, "current_password": {pw}}, sess)
}

func freshCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "panel_admin" && c.Value != "" {
			return c
		}
	}
	return nil
}

// SEC-CR-003: sign-out-elsewhere asks for the current password too.
func TestSettings_SignOutOtherDevices(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	here := e.login(t, "owner@example.com", owner0PW)
	elsewhere := e.login(t, "owner@example.com", owner0PW)
	time.Sleep(5 * time.Millisecond)

	if rec := e.signOutOthers(t, here, "wrong-password"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong password: status %d, want 403", rec.Code)
	}
	if e.do(t, http.MethodGet, "/admin/settings/profile/", nil, elsewhere).Code != http.StatusOK {
		t.Fatal("a wrong password signed the other device out")
	}
	rec := e.signOutOthers(t, here, owner0PW)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/settings/security/?sessions=1" {
		t.Fatalf("status %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	fresh := freshCookie(rec)
	if fresh == nil || e.do(t, http.MethodGet, "/admin/settings/profile/", nil, fresh).Code != http.StatusOK {
		t.Error("this browser was signed out too")
	}
	if e.do(t, http.MethodGet, "/admin/settings/profile/", nil, elsewhere).Code == http.StatusOK {
		t.Error("the other device is still signed in")
	}
	if got := e.do(t, http.MethodPost, "/admin/settings/security/sessions/", url.Values{"current_password": {owner0PW}}, fresh); got.Code != http.StatusForbidden {
		t.Errorf("sessions POST without a token = %d, want 403", got.Code)
	}
}

// SEC-CR-001 (both reviewers): a request that passed Require BEFORE the victim's
// password change must not mint a session AFTER it. The attacker holds a stolen
// cookie (and, worst case, the password); their POST is held inside the handler
// while the victim changes the password; released, it must be refused.
//
// Falsification: in resource/settings.go epochRequest, hand the store
// math.MaxInt64 instead of the session's IssuedAtMs (an unconditional write, the
// pre-fix behaviour) -> RED: the attacker's fresh cookie is valid.
func TestSettings_InFlightRevokedSessionCannotMintASession(t *testing.T) {
	for name, attack := range map[string]func(e *settingsEnv, stolen *http.Cookie) *httptest.ResponseRecorder{
		"sign-out-others": func(e *settingsEnv, stolen *http.Cookie) *httptest.ResponseRecorder {
			return e.signOutOthers(t, stolen, owner0PW)
		},
		"change-password": func(e *settingsEnv, stolen *http.Cookie) *httptest.ResponseRecorder {
			return e.changePW(t, stolen, pwForm(owner0PW, "attacker-chosen-pw-1", "attacker-chosen-pw-1"))
		},
	} {
		e := newSettingsEnv(t, settingsOpts{})
		victim := e.login(t, "owner@example.com", owner0PW)
		stolen := e.login(t, "owner@example.com", owner0PW)
		time.Sleep(5 * time.Millisecond)

		reached, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		e.store.beforeStamp = func() {
			held := false
			once.Do(func() { held = true; close(reached) })
			if held {
				<-release
			}
		}
		var attackerRec *httptest.ResponseRecorder
		done := make(chan struct{})
		go func() { defer close(done); attackerRec = attack(e, stolen) }()
		<-reached // the attacker is past Require, CSRF and the password check

		// The victim reacts to the compromise by changing the password.
		vrec := e.changePW(t, victim, pwForm(owner0PW, newPW, newPW))
		if vrec.Code != http.StatusSeeOther {
			t.Fatalf("%s: the victim's change failed: %d", name, vrec.Code)
		}
		victimNow := freshCookie(vrec)
		close(release)
		<-done

		if attackerRec.Code != http.StatusSeeOther || attackerRec.Header().Get("Location") != "/admin/login" {
			t.Errorf("%s: the stale in-flight request got %d -> %q, want a redirect to /admin/login", name, attackerRec.Code, attackerRec.Header().Get("Location"))
		}
		if c := freshCookie(attackerRec); c != nil && e.do(t, http.MethodGet, "/admin/settings/profile/", nil, c).Code == http.StatusOK {
			t.Errorf("%s: a revoked session minted a fresh valid one after the victim changed the password", name)
		}
		if !auth.VerifyPassword(newPW, e.store.hash("own")) {
			t.Errorf("%s: the attacker's request overwrote the victim's new password", name)
		}
		if e.do(t, http.MethodGet, "/admin/settings/profile/", nil, victimNow).Code != http.StatusOK {
			t.Errorf("%s: the victim lost their own session", name)
		}
	}
}

// SEC-CR-004: two password changes from one session at once: exactly one wins.
func TestSettings_ConcurrentPasswordChangesExactlyOneWins(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	time.Sleep(5 * time.Millisecond)
	var arrived sync.WaitGroup
	arrived.Add(2)
	e.store.beforeStamp = func() { arrived.Done(); arrived.Wait() }

	codes := make([]string, 2)
	var wg sync.WaitGroup
	for i, pw := range []string{"first-new-password-11", "second-new-password-22"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := e.changePW(t, sess, pwForm(owner0PW, pw, pw))
			codes[i] = rec.Header().Get("Location")
		}()
	}
	wg.Wait()
	ok, login := 0, 0
	for _, loc := range codes {
		switch loc {
		case "/admin/settings/security/?changed=1":
			ok++
		case "/admin/login":
			login++
		}
	}
	if ok != 1 || login != 1 {
		t.Errorf("outcomes %q: want one success and one redirect to login", codes)
	}
}

// SEC-CR-003: the throttle counts FAILED current-password checks only. Successes
// and CSRF failures do not use the quota.
//
// Falsification: in resource/settings.go throttleGate make the BlockedChecker
// branch unreachable (`ok && a.rate.Limit < 0`), so every attempt counts -> RED
// on the third success.
func TestSettings_ThrottleCountsOnlyFailedPasswordChecks(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{rate: auth.RateRule{Limit: 2, Window: time.Hour}})
	sess := e.login(t, "owner@example.com", owner0PW)
	for range 5 { // forged CSRF: not a password attempt
		e.do(t, http.MethodPost, "/admin/settings/security/sessions/", url.Values{"_csrf": {"forged"}, "current_password": {"x"}}, sess)
	}
	for i := range 4 { // successes: not failures
		time.Sleep(3 * time.Millisecond)
		rec := e.signOutOthers(t, sess, owner0PW)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("success %d: status %d (successes or CSRF failures used the quota)", i, rec.Code)
		}
		sess = freshCookie(rec)
	}
	time.Sleep(3 * time.Millisecond)
	for range 2 {
		if rec := e.signOutOthers(t, sess, "wrong"); rec.Code != http.StatusForbidden {
			t.Fatalf("failure: status %d", rec.Code)
		}
	}
	if rec := e.signOutOthers(t, sess, owner0PW); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after 2 failures the right password got %d, want 429", rec.Code)
	}
}

// A change that cannot re-issue this browser's session must not look like a
// success.
func TestSettings_NoReplacementSessionIsNotShownAsSuccess(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	time.Sleep(5 * time.Millisecond)
	e.store.breakAfterStamp = true
	rec := e.changePW(t, sess, pwForm(owner0PW, newPW, newPW))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Пароль изменён, войдите заново.") || strings.Contains(body, "На остальных устройствах вы вышли") {
		t.Errorf("status %d, body states the sign-in-again message: %v", rec.Code, strings.Contains(body, "войдите заново"))
	}
	if freshCookie(rec) != nil {
		t.Error("a cookie was minted although the replacement failed")
	}
	if !auth.VerifyPassword(newPW, e.store.hash("own")) {
		t.Error("the change should have happened")
	}
}

// A store without auth.EpochStore gets no password change and no sign-out-
// elsewhere: the rows are hidden and the routes do not exist (fail closed).
func TestSettings_StoreWithoutEpochStoreOffersNoPasswordOrSessionRows(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{noEpoch: true})
	sess := e.login(t, "owner@example.com", owner0PW)
	if rec := e.do(t, http.MethodGet, "/admin/settings/security/", nil, sess); rec.Code != http.StatusNotFound {
		t.Errorf("security page = %d with nothing to show, want 404", rec.Code)
	}
	if b := e.do(t, http.MethodGet, "/admin/settings/profile/", nil, sess).Body.String(); strings.Contains(b, "/admin/settings/security/") {
		t.Error("the sub-nav links a Security page that has no rows")
	}
	for _, path := range []string{"/admin/settings/security/password/", "/admin/settings/security/sessions/"} {
		rec := e.do(t, http.MethodPost, path, url.Values{"_csrf": {e.token(sess)}, "current_password": {owner0PW}}, sess)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want it not to exist", path, rec.Code)
		}
	}
}

// M2: a consumer page's POST inherits the page's role.
//
// Falsification: in resource/settings.go MountSettingsAction replace
// `spec.RequiredRole = entry.RequiredRole` with `spec.RequiredRole = ""` -> RED
// (the manager's POST runs the handler).
func TestSettings_ActionInheritsThePagesRole(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	mgr := e.login(t, "manager@example.com", "manager-password-1")
	own := e.login(t, "owner@example.com", owner0PW)
	post := func(c *http.Cookie) int {
		return e.do(t, http.MethodPost, "/admin/settings/collection/toggle", url.Values{"_csrf": {e.token(c)}}, c).Code
	}
	if code := post(mgr); code != http.StatusForbidden {
		t.Errorf("manager POST to the owner-only action = %d, want 403", code)
	}
	if e.actionRuns.Load() != 0 {
		t.Error("the owner-only handler ran for a manager")
	}
	if code := post(own); code != http.StatusNoContent || e.actionRuns.Load() != 1 {
		t.Errorf("owner POST = %d (runs %d), want 204 and one run", code, e.actionRuns.Load())
	}
	if code := e.do(t, http.MethodPost, "/admin/settings/collection/toggle", url.Values{}, own).Code; code != http.StatusForbidden {
		t.Errorf("owner POST without CSRF = %d, want 403", code)
	}
}

// No password and no hash appears in a response or a log line, whatever
// failure the form hits.
func TestSettings_NoPasswordLeaks(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	e := newSettingsEnv(t, settingsOpts{})
	sess := e.login(t, "owner@example.com", owner0PW)
	secrets := []string{"typed-wrong-current-77", "typed-new-secret-88", "typed-other-repeat-99", "tiny"}
	var bodies strings.Builder
	for _, form := range []url.Values{
		pwForm(secrets[0], secrets[1], secrets[1]),
		pwForm(owner0PW, secrets[1], secrets[2]),
		pwForm(owner0PW, secrets[3], secrets[3]),
	} {
		rec := e.changePW(t, sess, form)
		bodies.WriteString(rec.Body.String())
		for k, v := range rec.Header() {
			bodies.WriteString(k + strings.Join(v, ""))
		}
	}
	all := bodies.String() + logs.String()
	for _, s := range append(secrets[:3], owner0PW, e.store.hash("own")) {
		if strings.Contains(all, s) {
			t.Errorf("a password or hash (%d chars, %.3q…) appears in a response or log", len(s), s)
		}
	}
	if strings.Contains(all, "$2a$") || strings.Contains(all, "$2b$") {
		t.Error("a bcrypt hash appears in a response or log")
	}
	if page := e.do(t, http.MethodGet, "/admin/settings/security/", nil, sess).Body.String(); strings.Contains(page, `value="`+owner0PW) {
		t.Error("the form echoes a password")
	}
}

func TestSettings_SecurityShowsTwoFactorLinksWhenMounted(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{totp: true})
	sess := e.login(t, "owner@example.com", owner0PW)
	body := e.do(t, http.MethodGet, "/admin/settings/security/", nil, sess).Body.String()
	for _, want := range []string{"Двухфакторный вход", "Выключен", `href="/admin/security/totp/enroll/"`, "Сменить пароль", "Сеансы"} {
		if !strings.Contains(body, want) {
			t.Errorf("security page lacks %q", want)
		}
	}
	e.store.byID["own"].TOTPEnabled = true
	body = e.do(t, http.MethodGet, "/admin/settings/security/", nil, sess).Body.String()
	for _, want := range []string{"Включён", `href="/admin/security/totp/disable/"`, `href="/admin/security/totp/regenerate/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("security page (2FA on) lacks %q", want)
		}
	}
	plain := newSettingsEnv(t, settingsOpts{})
	if b := plain.do(t, http.MethodGet, "/admin/settings/security/", nil, plain.login(t, "owner@example.com", owner0PW)).Body.String(); strings.Contains(b, "Двухфакторный") {
		t.Error("a two-factor row without MountTOTPEnrollment")
	}
}

func TestSettings_AppearanceRows(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{skin: shell.SkinIris, skins: []string{shell.SkinGraphite}})
	sess := e.login(t, "owner@example.com", owner0PW)
	body := e.do(t, http.MethodGet, "/admin/settings/appearance/", nil, sess).Body.String()
	// F6 over HTTP: every registry skin allowed is an option, the default first.
	i, g := strings.Index(body, `name="theme-skin" value="iris"`), strings.Index(body, `name="theme-skin" value="graphite"`)
	if i < 0 || g < 0 || i > g {
		t.Errorf("want iris (default) then graphite options, got %d, %d", i, g)
	}
	for _, want := range []string{"Тема", "Режим", "Светлая", "Тёмная", "Ирис", "Графит", `name="theme-mode" value="light" checked`} {
		if !strings.Contains(body, want) {
			t.Errorf("appearance page lacks %q", want)
		}
	}
	// No allowed skins: no Тема row, the mode row stays.
	off := newSettingsEnv(t, settingsOpts{})
	b := off.do(t, http.MethodGet, "/admin/settings/appearance/", nil, off.login(t, "owner@example.com", owner0PW)).Body.String()
	if strings.Contains(b, "theme-skin") || strings.Contains(b, "Как выглядит панель") || strings.Count(b, `class="gp-set-row"`) != 1 || !strings.Contains(b, `name="theme-mode"`) {
		t.Error("without Config.Skins the Тема row must be absent and Режим present")
	}
}

func TestSettings_ConfigErrorsAreLoud(t *testing.T) {
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: want a setup-time panic", name)
			}
		}()
		f()
	}
	mk := func() *resource.Panel {
		base := newTOTPTestStore()
		a := i18nBcryptAuth(base, nil, auth.RateRule{})
		return resource.New(resource.Config{Title: "P", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey})
	}
	page := func(id string) resource.SettingsPage {
		return resource.SettingsPage{ID: id, Title: "T", Body: templ.Raw("x")}
	}
	mustPanic("twice", func() {
		p := mk()
		p.MountSettings(resource.SettingsConfig{})
		p.MountSettings(resource.SettingsConfig{})
	})
	mustPanic("built-in id", func() { mk().MountSettings(resource.SettingsConfig{Pages: []resource.SettingsPage{page("security")}}) })
	mustPanic("duplicate id", func() {
		mk().MountSettings(resource.SettingsConfig{Pages: []resource.SettingsPage{page("a"), page("a")}})
	})
	mustPanic("bad id", func() { mk().MountSettings(resource.SettingsConfig{Pages: []resource.SettingsPage{page("Bad/Id")}}) })
	mustPanic("no body", func() {
		mk().MountSettings(resource.SettingsConfig{Pages: []resource.SettingsPage{{ID: "x", Title: "T"}}})
	})
	mustPanic("store without csrf key", func() {
		base := newTOTPTestStore()
		a := i18nBcryptAuth(base, nil, auth.RateRule{})
		resource.New(resource.Config{Title: "P", BasePath: "/admin", Auth: a}).MountSettings(resource.SettingsConfig{Store: base})
	})
	noop := func(http.ResponseWriter, *http.Request) {}
	mustPanic("action before MountSettings", func() { mk().MountSettingsAction("x", resource.ActionSpec{Path: "a", Handler: noop}) })
	mustPanic("action on an unknown page", func() {
		p := mk()
		p.MountSettings(resource.SettingsConfig{})
		p.MountSettingsAction("nope", resource.ActionSpec{Path: "a", Handler: noop})
	})
	mustPanic("action on a built-in page", func() {
		p := mk()
		p.MountSettings(resource.SettingsConfig{})
		p.MountSettingsAction("security", resource.ActionSpec{Path: "a", Handler: noop})
	})
	mustPanic("action naming another role", func() {
		p := mk()
		p.MountSettings(resource.SettingsConfig{Pages: []resource.SettingsPage{{ID: "x", Title: "T", Body: templ.Raw("x"), RequiredRole: auth.RoleOwner}}})
		p.MountSettingsAction("x", resource.ActionSpec{Path: "a", Handler: noop, RequiredRole: "manager"})
	})
	mustPanic("page collision", func() {
		p := mk()
		p.MountPage(resource.PageSpec{Path: "settings", Handler: func(http.ResponseWriter, *http.Request) {}})
		p.MountSettings(resource.SettingsConfig{})
	})
	mustPanic("unknown skin", func() {
		resource.New(resource.Config{Title: "P", Auth: i18nBcryptAuth(newTOTPTestStore(), nil, auth.RateRule{}), Skins: []string{"iirs"}})
	})
}
