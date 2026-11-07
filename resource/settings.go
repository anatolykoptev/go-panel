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
// caller's session after a password change revoked the old one.
type sessionRefresher interface {
	RefreshSession(w http.ResponseWriter, r *http.Request, userID string) error
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
	panel   *Panel
	store   auth.AccountStore
	pages   []settingsEntry
	limiter auth.RateLimiter
	rate    auth.RateRule
	url     string // {base}/settings/
}

// MountSettings mounts the settings area: {BasePath}/settings redirects to the
// first page you may see, and each page is {BasePath}/settings/{id}/ with a left
// sub-navigation (a scrolling row at phone width). Built-in pages, «Личные»:
//
//   - profile: name, e-mail, role, sign out. Needs a per-operator session.
//   - security: change password (needs cfg.Store) and the two-factor status with
//     a link into MountTOTPEnrollment's routes (needs that mounted).
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
// Call once, before the first Handler(). Panics (setup time, fail-closed) on a
// second call, a Store with no usable CSRFKey, an invalid or duplicate page ID,
// a page without Body, or a path collision with a Resource or MountPage.
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
	if cfg.Store != nil {
		p.MountPage(PageSpec{Path: settingsPath + "/" + settingsPasswordID, Method: http.MethodPost, Handler: a.changePassword})
		if _, ok := cfg.Store.(auth.SessionRevoker); ok {
			p.MountPage(PageSpec{Path: settingsPath + "/" + settingsSessionsID, Method: http.MethodPost, Handler: a.signOutOthers})
		}
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

// account returns the signed-in operator's current row, or nil when the panel
// has no per-operator identity (HMACAuth) or no Store.
func (a *settingsArea) account(r *http.Request) *auth.Account {
	if a.store == nil {
		return nil
	}
	sess, ok := auth.SessionFrom(r.Context())
	if !ok {
		return nil
	}
	acct, err := a.store.GetByID(r.Context(), sess.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "resource: settings could not load the session account", "err", err)
		return nil
	}
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
				return acct != nil && (a.store != nil || a.panel.totp != nil)
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
	if a.store != nil {
		sections = append(sections, components.SettingsSection{
			Title:    s.PasswordRow,
			Subtitle: s.PasswordHint,
			Body:     passwordForm(passwordFormData{Action: a.url + settingsPasswordID + "/", Token: a.panel.CSRFToken(r)}),
		})
	}
	if _, ok := a.store.(auth.SessionRevoker); ok {
		sections = append(sections, components.SettingsSection{
			Title:    s.SessionsRow,
			Subtitle: s.SessionsHint,
			Rows: []components.SettingsRow{{
				Label:   s.SignOutOthers,
				Control: postButton(a.url+settingsSessionsID+"/", a.panel.CSRFToken(r), s.Logout),
			}},
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

// signOutOthers ends every session but the caller's: the store stamps a new
// credential epoch (every older token is revoked, this one too) and the caller's
// session is re-issued above it.
func (a *settingsArea) signOutOthers(w http.ResponseWriter, r *http.Request) {
	shell.SecurityHeaders(w)
	acct := a.account(r)
	rev, ok := a.store.(auth.SessionRevoker)
	if acct == nil || !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !a.panel.verifyCSRFToken(w, r, "resource: settings sessions CSRF verification failed") {
		return
	}
	if err := rev.RevokeSessions(r.Context(), acct.ID); err != nil {
		slog.ErrorContext(r.Context(), "resource: revoke sessions failed", "account", acct.ID, "err", err)
		a.render(w, r, securityPageID, http.StatusInternalServerError, settingsFlash{Err: strs(r).ErrPasswordUnavailable})
		return
	}
	if rf, ok := a.panel.auth.(sessionRefresher); ok {
		if err := rf.RefreshSession(w, r, acct.ID); err != nil {
			slog.ErrorContext(r.Context(), "resource: re-issue session after sign-out-others failed", "account", acct.ID, "err", err)
		}
	}
	http.Redirect(w, r, a.url+"security/?sessions=1", http.StatusSeeOther)
}

// ---- change password --------------------------------------------------------

func (a *settingsArea) changePassword(w http.ResponseWriter, r *http.Request) {
	shell.SecurityHeaders(w)
	acct := a.account(r)
	if acct == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !a.panel.verifyCSRFToken(w, r, "resource: settings password CSRF verification failed") {
		return
	}
	s := strs(r)
	if !a.allowAttempt(w, r, acct.ID, s) {
		return
	}
	current, next, repeat := r.PostForm.Get("current_password"), r.PostForm.Get("new_password"), r.PostForm.Get("repeat_password")
	if next != repeat {
		a.render(w, r, securityPageID, http.StatusBadRequest, settingsFlash{Err: s.ErrPasswordMismatch})
		return
	}
	err := auth.ChangePassword(r.Context(), a.store, acct, current, next)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrReauthFailed):
		a.render(w, r, securityPageID, http.StatusForbidden, settingsFlash{Err: s.ErrPasswordCurrent})
		return
	case errors.Is(err, auth.ErrPasswordTooShort):
		a.render(w, r, securityPageID, http.StatusBadRequest, settingsFlash{Err: s.ErrPasswordShort(auth.MinPasswordLength)})
		return
	case errors.Is(err, auth.ErrPasswordTooLong):
		a.render(w, r, securityPageID, http.StatusBadRequest, settingsFlash{Err: s.ErrPasswordLong(auth.MaxPasswordBytes)})
		return
	case errors.Is(err, auth.ErrPasswordUnchanged):
		a.render(w, r, securityPageID, http.StatusBadRequest, settingsFlash{Err: s.ErrPasswordUnchanged})
		return
	default:
		// The error wraps store/hash failures only; auth.ChangePassword never
		// puts a password or hash into one.
		slog.ErrorContext(r.Context(), "resource: change password failed", "account", acct.ID, "err", err)
		a.render(w, r, securityPageID, http.StatusInternalServerError, settingsFlash{Err: s.ErrPasswordUnavailable})
		return
	}
	// The new credential epoch revoked every session, this one included. Keep the
	// operator signed in on this browser; every other session stays revoked.
	if rf, ok := a.panel.auth.(sessionRefresher); ok {
		if err := rf.RefreshSession(w, r, acct.ID); err != nil {
			slog.ErrorContext(r.Context(), "resource: re-issue session after password change failed", "account", acct.ID, "err", err)
		}
	}
	http.Redirect(w, r, a.url+"security/?changed=1", http.StatusSeeOther)
}

// allowAttempt applies the per-account throttle, fail-closed: a limiter error
// denies, like the login throttle.
func (a *settingsArea) allowAttempt(w http.ResponseWriter, r *http.Request, accountID string, s Strings) bool {
	ok, err := a.limiter.Allow(r.Context(), "pwchange:"+accountID, a.rate.Limit, a.rate.Window)
	if err != nil {
		slog.ErrorContext(r.Context(), "resource: password-change rate limiter error; denying", "err", err)
	}
	if err == nil && ok {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(a.rate.Window.Seconds()))))
	a.render(w, r, securityPageID, http.StatusTooManyRequests, settingsFlash{Err: s.ErrPasswordThrottled})
	return false
}

func itoa(n int) string { return strings.TrimSpace(strconv.Itoa(n)) }
