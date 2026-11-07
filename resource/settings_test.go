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
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

// settingsStore is the multi-user test store with a password write that really
// writes (the shared fixture's is a no-op) and the sign-out-everywhere hook.
type settingsStore struct {
	*totpTestStore
	mu         sync.Mutex
	failUpdate bool // UpdatePasswordHash returns an error (a store outage)
}

func (s *settingsStore) UpdatePasswordHash(_ context.Context, id, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failUpdate {
		return context.DeadlineExceeded
	}
	a, ok := s.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	now := time.Now()
	a.PasswordHash, a.PasswordChangedAt = hash, &now
	return nil
}

func (s *settingsStore) RevokeSessions(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	now := time.Now()
	a.PasswordChangedAt = &now
	return nil
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
	p     *resource.Panel
	h     http.Handler
	a     *auth.BcryptTOTPAuth
	store *settingsStore
}

type settingsOpts struct {
	skin    string
	skins   []string
	rate    auth.RateRule
	noStore bool
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
	if !o.noStore {
		cfg.Store = store
	}
	p.MountSettings(cfg)
	return &settingsEnv{p: p, h: p.Handler(), a: a, store: store}
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

func TestSettings_SignOutOtherDevices(t *testing.T) {
	e := newSettingsEnv(t, settingsOpts{})
	here := e.login(t, "owner@example.com", owner0PW)
	elsewhere := e.login(t, "owner@example.com", owner0PW)
	time.Sleep(5 * time.Millisecond)
	form := url.Values{"_csrf": {e.token(here)}}
	rec := e.do(t, http.MethodPost, "/admin/settings/security/sessions/", form, here)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	var fresh *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "panel_admin" {
			fresh = c
		}
	}
	if fresh == nil || e.do(t, http.MethodGet, "/admin/settings/profile/", nil, fresh).Code != http.StatusOK {
		t.Error("this browser was signed out too")
	}
	if e.do(t, http.MethodGet, "/admin/settings/profile/", nil, elsewhere).Code == http.StatusOK {
		t.Error("the other device is still signed in")
	}
	// CSRF-protected too.
	if got := e.do(t, http.MethodPost, "/admin/settings/security/sessions/", url.Values{}, fresh); got.Code != http.StatusForbidden {
		t.Errorf("sessions POST without a token = %d, want 403", got.Code)
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
	if strings.Contains(b, "theme-skin") || strings.Contains(b, "Как выглядит панель") || strings.Count(b, `class="set-row"`) != 1 || !strings.Contains(b, `name="theme-mode"`) {
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
	mustPanic("page collision", func() {
		p := mk()
		p.MountPage(resource.PageSpec{Path: "settings", Handler: func(http.ResponseWriter, *http.Request) {}})
		p.MountSettings(resource.SettingsConfig{})
	})
	mustPanic("unknown skin", func() {
		resource.New(resource.Config{Title: "P", Auth: i18nBcryptAuth(newTOTPTestStore(), nil, auth.RateRule{}), Skins: []string{"iirs"}})
	})
}
