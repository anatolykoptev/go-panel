package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/shell"
)

// Sub-navigation groups of the settings area.
const (
	// SettingsGroupPersonal holds the built-in pages about the signed-in
	// operator (Profile, Security, Appearance).
	SettingsGroupPersonal = "personal"
	// SettingsGroupWorkspace holds the pages a consumer contributes. It is the
	// default group of a SettingsPage.
	SettingsGroupWorkspace = "workspace"
)

const (
	settingsPath       = "settings"
	settingsPasswordID = "security/password"
	settingsSessionsID = "security/sessions"
	securityPageID     = "security"
	maxSettingsForm    = 4096
	// Defaults for the change-password throttle: 5 attempts per account per 15
	// minutes. Wrong-password guessing needs a live session, so this is a second
	// line behind it, not the login throttle.
	defaultPasswordAttempts = 5
	defaultPasswordWindow   = 15 * time.Minute
)

var settingsIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// SettingsPage is one sub-page of the settings area, reachable at
// {BasePath}/settings/{ID}/ and listed in the left sub-navigation.
type SettingsPage struct {
	// ID is the URL segment: lower-case letters, digits, '-' and '_'. Must not
	// collide with a built-in page (profile, security, appearance).
	ID string
	// Group is SettingsGroupPersonal or SettingsGroupWorkspace (the default).
	Group string
	// Title is the nav label and the page heading.
	Title string
	// Subtitle is an optional muted line under the heading.
	Subtitle string
	// RequiredRole gates the page and its nav link exactly like
	// Resource.RequiredRole ("" = any signed-in operator).
	RequiredRole string
	// Body is the page content: build it from the components primitives
	// (SettingsSectionView, SettingsRow, SwitchView). It is rendered with the
	// request's context, so shell.ThemeRows and i18n.From work inside it.
	Body templ.Component
	// BodyFunc is Body for a page whose content depends on the request (a list
	// read from a store). When set it wins over Body. One of the two is required.
	BodyFunc func(r *http.Request) templ.Component
	// A page that writes needs its own POST routes: mount them with
	// Panel.MountAction, which applies the guard, the role and CSRF.
}

// SettingsConfig configures MountSettings.
type SettingsConfig struct {
	// Store enables the account-backed rows: the e-mail on Profile and the
	// change-password form on Security. nil leaves them out. It must be the
	// store the authenticator uses, and the authenticator must be
	// *auth.BcryptTOTPAuth (HMACAuth has no per-operator identity).
	Store auth.AccountStore
	// Pages are the consumer's sub-pages, listed after the built-in ones.
	Pages []SettingsPage
	// RateLimiter throttles change-password attempts per account. nil =
	// an in-process auth.MemoryLimiter.
	RateLimiter auth.RateLimiter
	// PasswordRate is the throttle's rule. Zero = 5 attempts per 15 minutes.
	PasswordRate auth.RateRule
}

// sessionRefresher is implemented by *auth.BcryptTOTPAuth: re-issue the
// caller's session from the epoch a conditional stamp returned.
type sessionRefresher interface {
	RefreshSession(w http.ResponseWriter, r *http.Request, epoch time.Time) error
}

type settingsEntry struct {
	SettingsPage
	// title overrides Title with a localised one for built-in pages.
	title func(Strings) string
	// subtitle likewise.
	subtitle func(Strings) string
	// visible reports whether the page exists for this request. Built-ins use it
	// for their prerequisites; consumer pages for their RequiredRole.
	visible func(a *settingsArea, r *http.Request, acct *auth.Account) bool
	body    func(a *settingsArea, r *http.Request, acct *auth.Account, f settingsFlash) templ.Component
}

type settingsArea struct {
	panel *Panel
	store auth.AccountStore
	// epoch is the store when it implements auth.EpochStore. nil hides the
	// password change and sign-out-everywhere (they fail closed, never fall
	// back to an unconditional write).
	epoch auth.EpochAccountStore
	// attempts serialises gate -> verify -> record per account on the
	// BlockedChecker path, so a burst cannot all pass the gate before the first
	// failure is recorded.
	attempts keyedMutex
	pages    []settingsEntry
	limiter  auth.RateLimiter
	rate     auth.RateRule
	url      string // {base}/settings/
}

// MountSettings mounts the settings area: {BasePath}/settings redirects to the
// first page you may see, and each page is {BasePath}/settings/{id}/ with a left
// sub-navigation (a scrolling row at phone width). Built-in pages, «Личные»:
//
//   - profile: name, e-mail, role, sign out. Needs a per-operator session.
//   - security: change password and «sign out on other devices» (both need
//     cfg.Store to implement auth.EpochStore, as PgxAccountStore does; a store
//     that does not gets neither row, by design) and the two-factor status with a
//     link into MountTOTPEnrollment's routes (needs that mounted).
//   - appearance: the skin choice (Config.Skins, two or more) and light / dark.
//
// then cfg.Pages in their group. A page whose prerequisites are missing does not
// exist (404, no nav link). Any signed-in operator may open the area; every
// built-in page is about the operator's own account.
//
// It also points the sidebar's Settings item at the area (ProfileConfig
// .SettingsURL when the consumer left it empty) and gives the single-user
// sidebar the same item. A panel that never calls MountSettings serves no
// route and renders byte-identically to before.
//
// A consumer page's POST routes are mounted with MountSettingsAction, which
// gives them the page's RequiredRole, the guard and CSRF.
//
// Call once, before the first Handler(). Panics (setup time, fail-closed) on a
// second call, a Store with no usable CSRFKey, an invalid or duplicate page ID,
// a page without Body, a path collision with a Resource or MountPage, and, via
// MountPage's own check, a page with a RequiredRole when the authenticator does
// not implement RoleAuthenticator.
func (p *Panel) MountSettings(cfg SettingsConfig) {
	if p.settings != nil {
		panic("resource: MountSettings called twice")
	}
	a := &settingsArea{panel: p, store: cfg.Store, limiter: cfg.RateLimiter, rate: cfg.PasswordRate, url: p.basePath + "/" + settingsPath + "/"}
	if cfg.Store != nil {
		if _, ok := p.auth.(sessionCookier); !ok {
			panic("resource.MountSettings: the authenticator has no SessionCookieName(); the password form's CSRF token cannot bind to the session")
		}
		if len(p.csrfKey) < minCSRFKeyLen {
			panic(fmt.Sprintf("resource.MountSettings: Config.CSRFKey must be at least %d bytes: the change-password POST is CSRF-protected", minCSRFKeyLen))
		}
	}
	a.epoch, _ = cfg.Store.(auth.EpochAccountStore)
	if a.limiter == nil {
		a.limiter = auth.NewMemoryLimiter()
	}
	if a.rate.Limit <= 0 || a.rate.Window <= 0 {
		a.rate = auth.RateRule{Limit: defaultPasswordAttempts, Window: defaultPasswordWindow}
	}
	a.pages = append(builtinSettingsPages(), a.consumerEntries(cfg.Pages)...)
	p.settings = a

	// Routes. MountPage records the paths, so a later resource or page with the
	// same suffix is caught by its own collision check.
	root := func(w http.ResponseWriter, r *http.Request) { a.redirectToFirst(w, r) }
	p.MountPage(PageSpec{Path: settingsPath, Aliases: []string{settingsPath}, Handler: root})
	for _, e := range a.pages {
		p.MountPage(PageSpec{Path: settingsPath + "/" + e.ID, RequiredRole: e.RequiredRole, Handler: a.pageHandler(e)})
	}
	if a.epoch != nil {
		p.MountPage(PageSpec{Path: settingsPath + "/" + settingsPasswordID, Method: http.MethodPost, Handler: a.changePassword})
		p.MountPage(PageSpec{Path: settingsPath + "/" + settingsSessionsID, Method: http.MethodPost, Handler: a.signOutOthers})
	}
	if p.profileCfg.SettingsURL == "" {
		p.settingsURL = a.url
	}
}

func (a *settingsArea) consumerEntries(pages []SettingsPage) []settingsEntry {
	seen := map[string]bool{"profile": true, securityPageID: true, "appearance": true}
	var out []settingsEntry
	for _, pg := range pages {
		switch {
		case !settingsIDRE.MatchString(pg.ID):
			panic(fmt.Sprintf("resource.MountSettings: page ID %q must match %s", pg.ID, settingsIDRE))
		case seen[pg.ID]:
			panic(fmt.Sprintf("resource.MountSettings: page ID %q is used twice or is a built-in page", pg.ID))
		case pg.Body == nil && pg.BodyFunc == nil:
			panic(fmt.Sprintf("resource.MountSettings: page %q has no Body or BodyFunc", pg.ID))
		case pg.Title == "":
			panic(fmt.Sprintf("resource.MountSettings: page %q has no Title", pg.ID))
		}
		if pg.Group != SettingsGroupPersonal {
			pg.Group = SettingsGroupWorkspace
		}
		seen[pg.ID] = true
		body := func(r *http.Request) templ.Component {
			if pg.BodyFunc != nil {
				return pg.BodyFunc(r)
			}
			return pg.Body
		}
		out = append(out, settingsEntry{
			SettingsPage: pg,
			visible: func(a *settingsArea, r *http.Request, _ *auth.Account) bool {
				return a.roleOK(r.Context(), pg.RequiredRole)
			},
			body: func(_ *settingsArea, r *http.Request, _ *auth.Account, _ settingsFlash) templ.Component {
				return body(r)
			},
		})
	}
	return out
}

func (a *settingsArea) roleOK(ctx context.Context, role string) bool {
	if role == "" {
		return true
	}
	ra, ok := a.panel.auth.(RoleAuthenticator)
	return ok && ra.HasRole(ctx, role)
}

// identity returns the signed-in operator's current row and the issue time (Unix
// ms) of the session that asked, or (nil, 0) when the panel has no per-operator
// identity (HMACAuth) or no Store.
func (a *settingsArea) identity(r *http.Request) (*auth.Account, int64) {
	if a.store == nil {
		return nil, 0
	}
	sess, ok := auth.SessionFrom(r.Context())
	if !ok {
		return nil, 0
	}
	acct, err := a.store.GetByID(r.Context(), sess.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "resource: settings could not load the session account", "err", err)
		return nil, 0
	}
	return acct, sess.IssuedAtMs
}

func (a *settingsArea) account(r *http.Request) *auth.Account {
	acct, _ := a.identity(r)
	return acct
}

func (a *settingsArea) visiblePages(r *http.Request, acct *auth.Account) []settingsEntry {
	var out []settingsEntry
	for _, e := range a.pages {
		if e.visible(a, r, acct) {
			out = append(out, e)
		}
	}
	return out
}

func (a *settingsArea) redirectToFirst(w http.ResponseWriter, r *http.Request) {
	pages := a.visiblePages(r, a.account(r))
	if len(pages) == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, a.url+pages[0].ID+"/", http.StatusFound)
}

func (a *settingsArea) nav(strs Strings, pages []settingsEntry, activeID string) []components.SettingsNavGroup {
	personal := components.SettingsNavGroup{Label: strs.SettingsGroupPersonal}
	work := components.SettingsNavGroup{Label: strs.SettingsGroupWorkspace}
	for _, e := range pages {
		it := components.SettingsNavItem{Title: e.titleFor(strs), URL: a.url + e.ID + "/", Active: e.ID == activeID}
		if e.Group == SettingsGroupPersonal {
			personal.Items = append(personal.Items, it)
		} else {
			work.Items = append(work.Items, it)
		}
	}
	var out []components.SettingsNavGroup
	for _, g := range []components.SettingsNavGroup{personal, work} {
		if len(g.Items) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func (e settingsEntry) titleFor(s Strings) string {
	if e.title != nil {
		return e.title(s)
	}
	return e.Title
}

func (e settingsEntry) subtitleFor(s Strings) string {
	if e.subtitle != nil {
		return e.subtitle(s)
	}
	return e.Subtitle
}

func (a *settingsArea) pageHandler(e settingsEntry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flash := settingsFlash{}
		if e.ID == securityPageID {
			q := r.URL.Query()
			if q.Get("changed") == "1" {
				flash.OK = strs(r).PasswordChanged
			}
			if q.Get("sessions") == "1" {
				flash.OK = strs(r).SessionsDone
			}
		}
		a.render(w, r, e.ID, http.StatusOK, flash)
	}
}

// statusOnWrite makes the first body write carry status, after RenderPage has
// set its headers.
type statusOnWrite struct {
	http.ResponseWriter
	status int
	sent   bool
}

func (w *statusOnWrite) Write(b []byte) (int, error) {
	if !w.sent {
		w.sent = true
		w.WriteHeader(w.status)
	}
	return w.ResponseWriter.Write(b)
}

func (a *settingsArea) render(w http.ResponseWriter, r *http.Request, id string, status int, flash settingsFlash) {
	acct := a.account(r)
	var entry *settingsEntry
	for _, e := range a.visiblePages(r, acct) {
		if e.ID == id {
			entry = &e
			break
		}
	}
	if entry == nil {
		http.NotFound(w, r)
		return
	}
	s := strs(r)
	pages := a.visiblePages(r, acct)
	layout := components.SettingsLayout{
		Heading: s.Settings, NavLabel: s.SettingsNavLabel, Nav: a.nav(s, pages, id),
		Title: entry.titleFor(s), Subtitle: entry.subtitleFor(s),
	}
	body := entry.body(a, r, acct, flash)
	content := templ.ComponentFunc(func(ctx context.Context, w2 io.Writer) error {
		return components.SettingsLayoutView(layout).Render(templ.WithChildren(ctx, body), w2)
	})
	sw := &statusOnWrite{ResponseWriter: w, status: status}
	// The sidebar heading is the panel's name, like every built-in page.
	if err := a.panel.RenderPage(sw, r, a.panel.title, "", content); err != nil {
		slog.ErrorContext(r.Context(), "resource: settings render failed", "page", id, "err", err)
	}
}

// ---- built-in pages ---------------------------------------------------------

func builtinSettingsPages() []settingsEntry {
	return []settingsEntry{
		{
			SettingsPage: SettingsPage{ID: "profile", Group: SettingsGroupPersonal},
			title:        func(s Strings) string { return s.SettingsProfile },
			subtitle:     func(s Strings) string { return s.ProfileHint },
			visible:      func(a *settingsArea, r *http.Request, _ *auth.Account) bool { return a.hasSession(r) },
			body:         profileBody,
		},
		{
			SettingsPage: SettingsPage{ID: securityPageID, Group: SettingsGroupPersonal},
			title:        func(s Strings) string { return s.SettingsSecurity },
			visible: func(a *settingsArea, _ *http.Request, acct *auth.Account) bool {
				return acct != nil && (a.epoch != nil || a.panel.totp != nil)
			},
			body: securityBody_,
		},
		{
			SettingsPage: SettingsPage{ID: "appearance", Group: SettingsGroupPersonal},
			title:        func(s Strings) string { return s.SettingsAppearance },
			visible:      func(*settingsArea, *http.Request, *auth.Account) bool { return true },
			body: func(_ *settingsArea, r *http.Request, _ *auth.Account, _ settingsFlash) templ.Component {
				return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
					return components.SettingsSectionView(components.SettingsSection{Rows: shell.ThemeRows(ctx)}).Render(ctx, w)
				})
			},
		},
	}
}

func (a *settingsArea) hasSession(r *http.Request) bool {
	_, ok := auth.SessionFrom(r.Context())
	return ok
}

func profileBody(a *settingsArea, r *http.Request, acct *auth.Account, _ settingsFlash) templ.Component {
	s := strs(r)
	prof := a.panel.profileFor(r)
	name, email, role := prof.Name, "", prof.Role
	if acct != nil {
		name, email, role = acct.Name, acct.Email, acct.Role
		if name == "" {
			name = prof.Name
		}
	}
	rows := []components.SettingsRow{{Label: s.SignedInAs(name)}, {Label: s.ProfileName, Control: statusWord(name)}}
	if email != "" {
		rows = append(rows, components.SettingsRow{Label: s.ProfileEmail, Control: statusWord(email)})
	}
	if role != "" {
		rows = append(rows, components.SettingsRow{Label: s.ProfileRole, Control: statusWord(role)})
	}
	rows = append(rows, components.SettingsRow{Label: s.Logout, Control: logoutButton(a.panel.basePath+"/logout", s.Logout)})
	return components.SettingsSectionView(components.SettingsSection{Rows: rows})
}

func securityBody_(a *settingsArea, r *http.Request, acct *auth.Account, f settingsFlash) templ.Component {
	s := strs(r)
	var sections []components.SettingsSection
	if a.epoch != nil {
		sections = append(sections, components.SettingsSection{
			Title:    s.PasswordRow,
			Subtitle: s.PasswordHint,
			Body:     passwordForm(passwordFormData{Action: a.url + settingsPasswordID + "/", Token: a.panel.CSRFToken(r)}),
		})
	}
	if a.epoch != nil {
		sections = append(sections, components.SettingsSection{
			Title:    s.SessionsRow,
			Subtitle: s.SessionsHint,
			Body:     sessionsForm(passwordFormData{Action: a.url + settingsSessionsID + "/", Token: a.panel.CSRFToken(r)}),
		})
	}
	if t := a.panel.totp; t != nil {
		row := components.SettingsRow{Label: s.TwoFactorOff, Control: linkButton(t.url("enroll"), s.TwoFactorEnable)}
		if acct.TOTPEnabled {
			row = components.SettingsRow{Label: s.TwoFactorOn, Control: linkPair(t.url("regenerate"), s.RecoveryRegenerateBtn, t.url("disable"), s.TOTPDisable)}
		}
		sections = append(sections, components.SettingsSection{Title: s.TwoFactorRow, Subtitle: s.TwoFactorHint, Rows: []components.SettingsRow{row}})
	}
	return securityBody(f, sections)
}

// epochRequest is the shared front half of the two epoch POSTs: it admits only
// a signed-in account on an EpochStore, caps and parses the body, verifies CSRF,
// and applies the per-account throttle. A rejected request has been answered.
func (a *settingsArea) epochRequest(w http.ResponseWriter, r *http.Request) (acct *auth.Account, iat int64, current string, release func(), ok bool) {
	shell.SecurityHeaders(w)
	acct, iat = a.identity(r)
	if acct == nil || a.epoch == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, 0, "", nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil, 0, "", nil, false
	}
	if !a.panel.verifyCSRFToken(w, r, "resource: settings CSRF verification failed") {
		return nil, 0, "", nil, false
	}
	// With a limiter that is checked without counting, gate, verify and record
	// must be one step per account: otherwise every request that reaches the
	// gate before the first failures are recorded (bcrypt takes ~200 ms) passes.
	release = func() {}
	if _, ok := a.limiter.(auth.BlockedChecker); ok {
		release = a.attempts.lock(acct.ID)
	}
	if !a.throttleGate(w, r, acct.ID) {
		release()
		return nil, 0, "", nil, false
	}
	return acct, iat, r.PostForm.Get("current_password"), release, true
}

// keyedMutex is a per-key lock whose entries exist only while held or awaited,
// so the map cannot grow with the number of accounts seen.
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*keyedEntry
}

type keyedEntry struct {
	mu   sync.Mutex
	refs int
}

// lock blocks until key is free and returns its unlock.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*keyedEntry{}
	}
	e := k.m[key]
	if e == nil {
		e = &keyedEntry{}
		k.m[key] = e
	}
	e.refs++
	k.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		k.mu.Lock()
		if e.refs--; e.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}

// epochFailed answers an auth.ChangePassword / RevokeOtherSessions error. It
// returns true when err was non-nil and the response has been written, false
// when the operation went through.
func (a *settingsArea) epochFailed(w http.ResponseWriter, r *http.Request, acct *auth.Account, err error) bool {
	s := strs(r)
	page := func(status int, msg string) { a.render(w, r, securityPageID, status, settingsFlash{Err: msg}) }
	switch {
	case err == nil:
		return false
	case errors.Is(err, auth.ErrReauthFailed):
		a.recordFailure(r, acct.ID)
		page(http.StatusForbidden, s.ErrPasswordCurrent)
	case errors.Is(err, auth.ErrSessionStale):
		// This session was revoked after Require admitted it (a password change
		// or sign-out elsewhere won). Nothing was written and nothing is minted.
		http.Redirect(w, r, a.panel.basePath+"/login", http.StatusSeeOther)
	case errors.Is(err, auth.ErrPasswordTooShort):
		page(http.StatusBadRequest, s.ErrPasswordShort(auth.MinPasswordLength))
	case errors.Is(err, auth.ErrPasswordTooLong):
		page(http.StatusBadRequest, s.ErrPasswordLong(auth.MaxPasswordBytes))
	case errors.Is(err, auth.ErrPasswordUnchanged):
		page(http.StatusBadRequest, s.ErrPasswordUnchanged)
	default:
		// Wraps store/hash failures only; the auth functions never put a
		// password or hash into an error.
		slog.ErrorContext(r.Context(), "resource: settings epoch operation failed", "account", acct.ID, "err", err)
		page(http.StatusInternalServerError, s.ErrPasswordUnavailable)
	}
	return true
}

// finishEpoch replaces the caller's session from the epoch the store stamped and
// redirects to okURL. When no replacement can be minted the change still
// happened, so it must not look like a success: the operator is told to sign in
// again.
func (a *settingsArea) finishEpoch(w http.ResponseWriter, r *http.Request, acct *auth.Account, epoch time.Time, okURL, signInMsg string) {
	rf, ok := a.panel.auth.(sessionRefresher)
	var err error
	if ok {
		err = rf.RefreshSession(w, r, epoch)
	}
	if !ok || err != nil {
		if err != nil {
			slog.ErrorContext(r.Context(), "resource: re-issuing the session after an epoch change failed", "account", acct.ID, "err", err)
		}
		a.renderSignInAgain(w, r, signInMsg)
		return
	}
	http.Redirect(w, r, okURL, http.StatusSeeOther)
}

func (a *settingsArea) renderSignInAgain(w http.ResponseWriter, r *http.Request, msg string) {
	sw := &statusOnWrite{ResponseWriter: w, status: http.StatusOK}
	content := signInAgain(msg, a.panel.basePath+"/login", strs(r).SignIn)
	if err := a.panel.RenderPage(sw, r, a.panel.title, "", content); err != nil {
		slog.ErrorContext(r.Context(), "resource: sign-in-again page failed", "err", err)
	}
}

// signOutOthers ends every session but the caller's, after the operator
// re-proves the current password: the epoch is stamped conditionally on this
// session still being current, and the caller's session is re-issued from the
// stamp the store returned.
func (a *settingsArea) signOutOthers(w http.ResponseWriter, r *http.Request) {
	acct, iat, current, release, ok := a.epochRequest(w, r)
	if !ok {
		return
	}
	defer release()
	epoch, err := auth.RevokeOtherSessions(r.Context(), a.epoch, acct, iat, current)
	if a.epochFailed(w, r, acct, err) {
		return
	}
	a.finishEpoch(w, r, acct, epoch, a.url+"security/?sessions=1", strs(r).SessionsDoneSignIn)
}

// changePassword is POST settings/security/password/.
func (a *settingsArea) changePassword(w http.ResponseWriter, r *http.Request) {
	acct, iat, current, release, ok := a.epochRequest(w, r)
	if !ok {
		return
	}
	defer release()
	next, repeat := r.PostForm.Get("new_password"), r.PostForm.Get("repeat_password")
	if next != repeat {
		a.render(w, r, securityPageID, http.StatusBadRequest, settingsFlash{Err: strs(r).ErrPasswordMismatch})
		return
	}
	epoch, err := auth.ChangePassword(r.Context(), a.epoch, acct, iat, current, next)
	if a.epochFailed(w, r, acct, err) {
		return
	}
	a.finishEpoch(w, r, acct, epoch, a.url+"security/?changed=1", strs(r).PasswordChangedSignIn)
}

// throttleGate is the per-account throttle, fail-closed. With a limiter that can
// answer "blocked?" without counting (auth.BlockedChecker, which the default
// in-process limiter is) only FAILED current-password checks count (see
// recordFailure): successes and CSRF failures do not. With one that cannot, every
// attempt that reaches this point counts, the conservative fallback.
func (a *settingsArea) throttleGate(w http.ResponseWriter, r *http.Request, accountID string) bool {
	key := "pwchange:" + accountID
	var blocked bool
	var err error
	if bc, ok := a.limiter.(auth.BlockedChecker); ok {
		blocked, err = bc.Blocked(r.Context(), key, a.rate.Limit, a.rate.Window)
	} else {
		var allowed bool
		allowed, err = a.limiter.Allow(r.Context(), key, a.rate.Limit, a.rate.Window)
		blocked = !allowed
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "resource: password-change rate limiter error; denying", "err", err)
	}
	if err == nil && !blocked {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(a.rate.Window.Seconds()))))
	a.render(w, r, securityPageID, http.StatusTooManyRequests, settingsFlash{Err: strs(r).ErrPasswordThrottled})
	return false
}

// recordFailure counts one failed current-password check, for limiters that
// throttleGate checks without counting.
func (a *settingsArea) recordFailure(r *http.Request, accountID string) {
	if _, ok := a.limiter.(auth.BlockedChecker); !ok {
		return // already counted by throttleGate
	}
	if _, err := a.limiter.Allow(r.Context(), "pwchange:"+accountID, a.rate.Limit, a.rate.Window); err != nil {
		slog.ErrorContext(r.Context(), "resource: recording a failed password check failed", "err", err)
	}
}

// MountSettingsAction mounts a POST route under a consumer settings page,
// {BasePath}/settings/{pageID}/{spec.Path}, for that page's forms and switches.
// It applies the PAGE'S RequiredRole (a spec that names a different one panics),
// the guard and CSRF verification with form parsing, exactly like MountAction,
// so a page's writes cannot be weaker than its reads. Call it after
// MountSettings and before Handler(); panics on an unknown or built-in page.
func (p *Panel) MountSettingsAction(pageID string, spec ActionSpec) {
	if p.settings == nil {
		panic("resource: MountSettingsAction called before MountSettings")
	}
	var entry *settingsEntry
	for i := range p.settings.pages {
		if p.settings.pages[i].ID == pageID {
			entry = &p.settings.pages[i]
		}
	}
	if entry == nil || entry.title != nil {
		panic(fmt.Sprintf("resource: MountSettingsAction: %q is not a consumer settings page", pageID))
	}
	if spec.RequiredRole != "" && spec.RequiredRole != entry.RequiredRole {
		panic(fmt.Sprintf("resource: MountSettingsAction: the action's RequiredRole %q differs from page %q's %q; actions inherit the page's role", spec.RequiredRole, pageID, entry.RequiredRole))
	}
	spec.RequiredRole = entry.RequiredRole
	spec.Path = settingsPath + "/" + pageID + "/" + strings.Trim(spec.Path, "/")
	p.MountAction(spec)
}

func itoa(n int) string { return strings.TrimSpace(strconv.Itoa(n)) }
