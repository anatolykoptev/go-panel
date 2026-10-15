package resource_test

// The page harness behind the i18n tests.
//
// pagesFor builds a representative admin (list, form, validation errors,
// detail, trash, sidebar, login, MFA, TOTP enrollment) and returns every
// rendered page. The same collection feeds three questions:
//
//   - do the DEFAULT strings still render byte-for-byte what v0.23.16 did
//     (golden files under testdata/i18n),
//   - does a consumer with RussianStrings() see any English left, and
//   - is every field of Strings actually consumed by some page.
//
// One collector for all three keeps the audited surface single: a page added
// here is audited by every test at once.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/locale"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
	"github.com/anatolykoptev/go-panel/tenant"
	"golang.org/x/crypto/bcrypt"
)

// i18nSeed is seedAccount at bcrypt.MinCost. The page harness runs three times
// (golden, Russian, markers) and each run logs in and re-authenticates a dozen
// times; at the production cost that is minutes under -race, enough to blow the
// default 10m package timeout. The password check itself is unchanged — bcrypt
// reads the cost out of the hash — and nothing here asserts on cost.
func i18nSeed(t *testing.T, store *testAccountStore, id, email, pw, role string) *auth.Account {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	a := &auth.Account{ID: id, Email: email, PasswordHash: string(h), Role: role, Active: true}
	store.add(a)
	return a
}

// i18nPage is one rendered response.
type i18nPage struct {
	name string
	code int
	body string
}

// i18nOpts tweaks the panel under test; the zero value is a consumer that
// configured nothing.
type i18nOpts struct {
	cfg func(*resource.Config)
	res func(*resource.Resource)
}

const i18nCSRFMaterial = "i18n-csrf-key-i18n-csrf-key-0123"

var i18nCSRFKey = []byte(i18nCSRFMaterial)

// trashMode steers what the kitchen resource's TrashLister reports.
type trashMode int

const (
	trashFull trashMode = iota
	trashEmpty
)

func i18nKitchen(mode *trashMode) resource.Resource {
	rows := []resource.Row{
		{ID: "1", Cells: []resource.Cell{{Value: "Альфа"}, {Value: "2026-01-01"}}},
		{ID: "2", Cells: []resource.Cell{{Value: "Бета"}, {Value: "2026-01-02"}}},
	}
	noop := func(context.Context, tenant.Tenant, string) error { return nil }
	return resource.Resource{
		Name: "items", Title: "Записи", Icon: "*", Group: "Данные",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Label: "Название", SQLExpr: "i.name", Sortable: true},
				{Key: "created", Label: "Создано", SQLExpr: "i.created_at"},
			},
			DefaultKey: "name", DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "status", SQLExpr: "i.status", Match: admintable.Eq, Allowed: []string{"new", "won"}},
			{Key: "q", SQLExprs: []string{"i.name"}, Match: admintable.ILike},
		}},
		FilterLabels: map[string]map[string]string{"status": {"new": "Новые", "won": "Выигранные"}},
		Views:        []resource.View{{Key: "all", Label: "Все"}, {Key: "mine", Label: "Мои"}},
		Lister: func(context.Context, resource.ListQuery) ([]resource.Row, int, error) {
			return rows, 120, nil
		},
		TrashLister: func(context.Context, resource.ListQuery) ([]resource.Row, int, error) {
			if *mode == trashEmpty {
				return nil, 0, nil
			}
			return rows, 60, nil
		},
		Detailer: func(context.Context, *http.Request, string) ([]resource.DetailSection, error) {
			return []resource.DetailSection{{Title: "Основное", Items: []resource.DetailItem{{Label: "Имя", Value: "Альфа"}}}}, nil
		},
		Writer: &resource.Writer{
			Form: resource.FormSpec{Fields: []resource.Field{
				{Key: "name", Label: "Название", Kind: resource.FieldText, Required: true, Translatable: true},
				{Key: "qty", Label: "Количество", Kind: resource.FieldNumber},
				{Key: "day", Label: "День", Kind: resource.FieldDate},
				{Key: "at", Label: "Момент", Kind: resource.FieldDateTime},
				{Key: "kind", Label: "Вид", Kind: resource.FieldSelect, Options: []resource.Option{{Value: "a", Label: "Первый"}}},
				{Key: "meta", Label: "Данные", Kind: resource.FieldJSON},
				{Key: "flag", Label: "Флаг", Kind: resource.FieldCheckbox},
			}},
			Load: func(context.Context, tenant.Tenant, string) (map[string]string, error) {
				return map[string]string{"name": "Альфа"}, nil
			},
			Save:    func(context.Context, tenant.Tenant, string, map[string]string) error { return nil },
			Delete:  noop,
			Restore: noop,
		},
	}
}

func i18nBrokenTrash(mode *trashMode) resource.Resource {
	r := i18nKitchen(mode)
	r.Name, r.Title, r.Group = "broken", "Сломанное", "Данные"
	r.TrashLister = func(context.Context, resource.ListQuery) ([]resource.Row, int, error) {
		if *mode == trashEmpty {
			return nil, 0, nil
		}
		return nil, 0, errors.New("trash store down")
	}
	return r
}

func i18nEmptyList() resource.Resource {
	return resource.Resource{
		Name: "nothing", Title: "Пусто", Group: "Данные",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "name", Label: "Название", SQLExpr: "n.name", Sortable: true}},
			DefaultKey: "name", DefaultDir: admintable.Asc,
		},
		Lister: func(context.Context, resource.ListQuery) ([]resource.Row, int, error) { return nil, 0, nil },
		Detailer: func(context.Context, *http.Request, string) ([]resource.DetailSection, error) {
			return nil, nil
		},
	}
}

func i18nRequest(method, target string, form url.Values, cookies []*http.Cookie, htmx bool) *http.Request {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequestWithContext(context.Background(), method, target, nil)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return req
}

// i18nRecorder collects pages in order.
type i18nRecorder struct {
	t     *testing.T
	pages []i18nPage
}

func (r *i18nRecorder) do(name string, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	r.t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	r.pages = append(r.pages, i18nPage{name: name, code: w.Code, body: w.Body.String()})
	return w
}

// i18nHMACPanel is a single-user panel with the empty-state index.
func i18nHMACPanel(o i18nOpts, withResource bool) (*resource.Panel, *auth.HMACAuth) {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "op", Password: "pw", HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/admin",
	})
	cfg := resource.Config{Title: "Панель", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey}
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	p := resource.New(cfg)
	if withResource {
		resource.Register(p, i18nEmptyList())
	}
	return p, a
}

func i18nHMACCookie(t *testing.T, a *auth.HMACAuth) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, i18nRequest(http.MethodPost, "/admin/login", url.Values{"username": {"op"}, "password": {"pw"}}, nil, false))
	for _, c := range w.Result().Cookies() {
		if c.Name == "panel_admin" {
			return c
		}
	}
	t.Fatal("no session cookie from HMAC login")
	return nil
}

func i18nBcryptAuth(store auth.AccountStore, limiter auth.RateLimiter, rate auth.RateRule) *auth.BcryptTOTPAuth {
	cfg := auth.BcryptConfig{
		Store: store, HMACKey: []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin", SessionTTL: time.Hour,
	}
	if _, ok := store.(auth.TOTPStore); ok {
		// A TOTP-capable store makes the constructor demand a key and a limiter.
		cfg.TOTPEncryptionKey = totpTestEncKey
		cfg.RateLimiter, cfg.LoginRate, cfg.TOTPRate = testRateLimiter, testLoginRate, testTOTPRate
	}
	if limiter != nil {
		cfg.RateLimiter, cfg.LoginRate, cfg.TOTPRate = limiter, rate, rate
	}
	return auth.NewBcryptTOTPAuth(cfg)
}

type i18nDenyAll struct{}

func (i18nDenyAll) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return false, nil
}

// i18nAllowLogin lets the password step through and throttles only the MFA step.
type i18nAllowLogin struct{ calls int }

func (l *i18nAllowLogin) Allow(context.Context, string, int, time.Duration) (bool, error) {
	l.calls++
	return l.calls == 1, nil
}

// pagesFor renders the whole audited surface under o.
func pagesFor(t *testing.T, o i18nOpts) []i18nPage {
	t.Helper()
	rec := &i18nRecorder{t: t}
	mode := trashFull

	i18nResourcePages(t, rec, o, &mode)
	i18nTOTPPages(t, rec, o)
	i18nLoginPages(t, rec, o)
	return rec.pages
}

func i18nResourcePages(t *testing.T, rec *i18nRecorder, o i18nOpts, mode *trashMode) {
	t.Helper()
	// Multi-user panel: the sidebar profile and Settings link render here.
	store := newTOTPTestStore()
	i18nSeed(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	a := i18nBcryptAuth(store, nil, auth.RateRule{})
	cfg := resource.Config{
		Title: "Панель", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey,
		Locales: mustLocales(t),
	}
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	p := resource.New(cfg)
	p.SetProfile(shell.ProfileConfig{SettingsURL: "/admin/settings"})
	kitchen := i18nKitchen(mode)
	if o.res != nil {
		o.res(&kitchen)
	}
	resource.Register(p, kitchen)
	resource.Register(p, i18nBrokenTrash(mode))
	resource.Register(p, i18nEmptyList())
	h := p.Handler()
	ck := bcryptLogin(t, a, "op@example.com", "pw")
	cks := []*http.Cookie{ck}
	tok := csrf.Issue(i18nCSRFKey, ck.Value, csrf.DefaultTTL)

	get := func(name, target string, htmx bool) {
		rec.do(name, h, i18nRequest(http.MethodGet, target, nil, cks, htmx))
	}
	post := func(name, target string, form url.Values, htmx bool) {
		form.Set("_csrf", tok)
		rec.do(name, h, i18nRequest(http.MethodPost, target, form, cks, htmx))
	}

	get("list", "/admin/items", false)
	get("list-rows-page2", "/admin/items/rows?page=2", true)
	get("list-rows-append", "/admin/items/rows?page=2&append=1", true)
	get("list-restorable", "/admin/items?restorable=7", false)
	get("list-empty", "/admin/nothing", false)
	get("form-new", "/admin/items/new", false)
	get("form-edit", "/admin/items/1/edit", false)
	get("form-edit-secondary-locale", "/admin/items/1/edit?locale=en", false)
	post("form-invalid", "/admin/items/new/save", url.Values{
		"name": {""}, "qty": {"abc"}, "day": {"x"}, "at": {"x"}, "kind": {"zzz"}, "meta": {"{"}, "flag": {"maybe"},
	}, false)
	get("detail", "/admin/items/1", false)
	get("detail-empty", "/admin/nothing/1", false)
	post("delete-row", "/admin/items/1/delete", url.Values{}, true)
	get("trash", "/admin/trash", false)
	*mode = trashEmpty
	get("trash-empty", "/admin/trash", false)
	*mode = trashFull

	// Single-user panel: bare Logout footer, and the empty-state index.
	hp, ha := i18nHMACPanel(o, true)
	hck := i18nHMACCookie(t, ha)
	rec.do("list-hmac-bare-sidebar", hp.Handler(), i18nRequest(http.MethodGet, "/admin/nothing", nil, []*http.Cookie{hck}, false))
	ep, ea := i18nHMACPanel(o, false)
	eck := i18nHMACCookie(t, ea)
	rec.do("index-empty", ep.Handler(), i18nRequest(http.MethodGet, "/admin/", nil, []*http.Cookie{eck}, false))
}

func mustLocales(t *testing.T) locale.Set {
	t.Helper()
	s, err := locale.NewSet("ru", "ru", "en")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func i18nTOTPPages(t *testing.T, rec *i18nRecorder, o i18nOpts) {
	t.Helper()
	store := newTOTPTestStore()
	i18nSeed(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	a := auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store: store, HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/admin",
		SessionTTL: time.Hour, TOTPEncryptionKey: totpTestEncKey,
		RateLimiter: testRateLimiter, LoginRate: testLoginRate, TOTPRate: testTOTPRate,
	})
	cfg := resource.Config{Title: "Панель", BasePath: "/admin", Auth: a, CSRFKey: totpTestCSRFKey}
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	p := resource.New(cfg)
	resource.MountTOTPEnrollment(p, resource.TOTPEnrollmentConfig{
		Store: store, TOTPEncryptionKey: totpTestEncKey, Issuer: totpTestIssuer, PathPrefix: totpTestPrefix,
	})
	h := p.Handler()
	ck := bcryptLogin(t, a, "op@example.com", "pw")
	cks := []*http.Cookie{ck}
	tok := csrf.Issue(totpTestCSRFKey, ck.Value, csrf.DefaultTTL)
	base := "/admin/" + totpTestPrefix + "/"
	post := func(name, path string, form url.Values) *httptest.ResponseRecorder {
		form.Set("_csrf", tok)
		return rec.do(name, h, i18nRequest(http.MethodPost, base+path, form, cks, false))
	}
	get := func(name, path string) {
		rec.do(name, h, i18nRequest(http.MethodGet, base+path, nil, cks, false))
	}

	get("totp-enroll-gate", "enroll/")
	post("totp-enroll-wrong-password", "enroll/", url.Values{"current_password": {"nope"}})
	w := post("totp-enroll", "enroll/", url.Values{"current_password": {"pw"}})
	secret := totpSecretFromPage(t, w.Body.String())
	post("totp-confirm-bad-code", "confirm/", url.Values{"code": {"000000"}, "current_password": {"pw"}})
	post("totp-confirm-ok", "confirm/", url.Values{
		"code": {validCodeForSecret(t, secret, time.Now())}, "current_password": {"pw"},
	})
	get("totp-already-enabled", "enroll/")
	get("totp-regenerate-gate", "regenerate/")
	post("totp-regenerate-wrong-password", "regenerate/", url.Values{"current_password": {"nope"}})
	post("totp-regenerate-ok", "regenerate/", url.Values{"current_password": {"pw"}})
	get("totp-disable-gate", "disable/")
	post("totp-disable-wrong-password", "disable/", url.Values{"current_password": {"nope"}})
	post("totp-disable-ok", "disable/", url.Values{"current_password": {"pw"}})
}

func totpSecretFromPage(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<code")
	if start < 0 {
		t.Fatalf("enroll page has no <code> secret: %s", body)
	}
	open := strings.Index(body[start:], ">") + start + 1
	end := strings.Index(body[open:], "</code>") + open
	return strings.TrimSpace(body[open:end])
}

func i18nLoginPages(t *testing.T, rec *i18nRecorder, o i18nOpts) {
	t.Helper()
	// HMAC: GET + failed POST.
	hp, _ := i18nHMACPanel(o, true)
	hh := hp.Handler()
	rec.do("login-hmac", hh, i18nRequest(http.MethodGet, "/admin/login", nil, nil, false))
	rec.do("login-hmac-bad", hh, i18nRequest(http.MethodPost, "/admin/login", url.Values{"username": {"x"}, "password": {"y"}}, nil, false))

	newBcryptPanel := func(a *auth.BcryptTOTPAuth) http.Handler {
		cfg := resource.Config{Title: "Панель", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey}
		if o.cfg != nil {
			o.cfg(&cfg)
		}
		return resource.New(cfg).Handler()
	}
	login := func(email, pw string) url.Values { return url.Values{"email": {email}, "password": {pw}} }

	store := newTOTPTestStore()
	i18nSeed(t, store.testAccountStore, "u1", "op@example.com", "pw", "admin")
	mfaAcct := i18nSeed(t, store.testAccountStore, "u2", "mfa@example.com", "pw", "admin")
	mfaAcct.TOTPEnabled = true

	bh := newBcryptPanel(i18nBcryptAuth(store, nil, auth.RateRule{}))
	rec.do("login-bcrypt", bh, i18nRequest(http.MethodGet, "/admin/login", nil, nil, false))
	rec.do("login-bcrypt-bad", bh, i18nRequest(http.MethodPost, "/admin/login", login("op@example.com", "wrong"), nil, false))
	w := rec.do("mfa", bh, i18nRequest(http.MethodPost, "/admin/login", login("mfa@example.com", "pw"), nil, false))
	mfaCookies := w.Result().Cookies()
	rec.do("mfa-bad-code", bh, i18nRequest(http.MethodPost, "/admin/login", url.Values{"code": {"000000"}}, mfaCookies, false))
	rec.do("mfa-empty-code", bh, i18nRequest(http.MethodPost, "/admin/login", url.Values{"code": {""}}, mfaCookies, false))

	// An MFA cookie whose account has since vanished.
	delete(store.byID, "u2")
	rec.do("mfa-invalid-session", bh, i18nRequest(http.MethodPost, "/admin/login", url.Values{"code": {"000000"}}, mfaCookies, false))

	// TOTPEnabled account but the authenticator has no TOTP wiring.
	store2 := newTestAccountStore()
	i18nSeed(t, store2, "u3", "t@example.com", "pw", "admin").TOTPEnabled = true
	nh := newBcryptPanel(i18nBcryptAuth(store2, nil, auth.RateRule{}))
	rec.do("login-2fa-unavailable", nh, i18nRequest(http.MethodPost, "/admin/login", login("t@example.com", "pw"), nil, false))

	// Throttled login.
	th := newBcryptPanel(i18nBcryptAuth(store, i18nDenyAll{}, auth.RateRule{Limit: 1, Window: time.Minute}))
	rec.do("login-throttled", th, i18nRequest(http.MethodPost, "/admin/login", login("op@example.com", "pw"), nil, false))

	// Throttled MFA step: password step passes the limiter once, MFA step is denied.
	store3 := newTOTPTestStore()
	i18nSeed(t, store3.testAccountStore, "u4", "m@example.com", "pw", "admin").TOTPEnabled = true
	mh := newBcryptPanel(i18nBcryptAuth(store3, &i18nAllowLogin{}, auth.RateRule{Limit: 1, Window: time.Minute}))
	w = rec.do("mfa-for-throttle", mh, i18nRequest(http.MethodPost, "/admin/login", login("m@example.com", "pw"), nil, false))
	rec.do("mfa-throttled", mh, i18nRequest(http.MethodPost, "/admin/login", url.Values{"code": {"000000"}}, w.Result().Cookies(), false))
}

// i18nNormalize removes the values that legitimately differ run to run (CSRF
// tokens carry an expiry, TOTP secrets and recovery codes are random).
var (
	csrfTokenRE     = regexp.MustCompile(`\d{10}\|[A-Za-z0-9_=+/-]+`)
	codeElementRE   = regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`)
	recoveryItemRE  = regexp.MustCompile(`<li style="user-select:all">[^<]*</li>`)
	totpDigitsEnrol = regexp.MustCompile(`otpauth://[^"'<\s]+`)
	// The theme toggle and the password reveal button gained attributes that
	// carry their translated labels to the (string-free) scripts. They are the
	// only intended addition to the default render; stripping them lets the
	// golden pin everything else byte for byte.
	// The stylesheet is inlined into every page (about 670 lines) and is pinned
	// by shell's own golden; repeating it per page would bury the markup.
	styleBlockRE  = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`)
	scriptLabelRE = regexp.MustCompile(` data-label-(?:to-light|to-dark|show|hide)="[^"]*"`)
)

func i18nNormalize(s string) string {
	// A literal apostrophe in a templ text node is emitted raw; the same text
	// as an expression is escaped to &#39;. Both parse to the same character,
	// so the golden compares the decoded form.
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = styleBlockRE.ReplaceAllString(s, "<style>CSS</style>")
	s = scriptLabelRE.ReplaceAllString(s, "")
	// ...and the login page's reveal script now reads the label from them.
	s = strings.ReplaceAll(s, "btn.getAttribute(show?'data-label-hide':'data-label-show')", "show?'Hide password':'Show password'")
	s = csrfTokenRE.ReplaceAllString(s, "CSRF")
	s = codeElementRE.ReplaceAllString(s, "<code>SECRET</code>")
	s = recoveryItemRE.ReplaceAllString(s, `<li style="user-select:all">CODE</li>`)
	s = totpDigitsEnrol.ReplaceAllString(s, "otpauth://X")
	return s
}

func i18nPagesDump(pages []i18nPage) map[string]string {
	out := make(map[string]string, len(pages))
	for _, p := range pages {
		var b bytes.Buffer
		fmt.Fprintf(&b, "HTTP %d\n%s", p.code, i18nNormalize(p.body))
		out[p.name] = b.String()
	}
	return out
}
