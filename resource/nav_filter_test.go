package resource_test

// Tests for Phase t3-3 (nav-filter via HasRole + Visible) and Phase t7 (profile block).
// See plan: ~/deploy/krolik-server/plans/go-panel/2026-06-27-shared-sidebar-fleet-standard-v2.md

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
	"github.com/anatolykoptev/go-panel/tenant"
)

// ── in-memory AccountStore for test-scoped role scenarios ──────────────────

type testAccountStore struct {
	byEmail map[string]*auth.Account
	byID    map[string]*auth.Account
}

func newTestAccountStore() *testAccountStore {
	return &testAccountStore{
		byEmail: map[string]*auth.Account{},
		byID:    map[string]*auth.Account{},
	}
}

func (s *testAccountStore) add(a *auth.Account) {
	s.byEmail[a.Email] = a
	s.byID[a.ID] = a
}

func (s *testAccountStore) GetByEmail(_ context.Context, email string) (*auth.Account, error) {
	a, ok := s.byEmail[email]
	if !ok || !a.Active || a.PasswordHash == "" {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *testAccountStore) GetByID(_ context.Context, id string) (*auth.Account, error) {
	a, ok := s.byID[id]
	if !ok {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *testAccountStore) UpdateLastLogin(context.Context, string) error        { return nil }
func (s *testAccountStore) UpdatePasswordHash(_ context.Context, _, _ string) error { return nil }
func (s *testAccountStore) CreateAccount(_ context.Context, _, _, _, _ string) (string, bool, error) {
	return "", false, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

// seedAccount adds a hashed-password account to store.
func seedAccount(t *testing.T, store *testAccountStore, id, email, pw, role string) *auth.Account {
	t.Helper()
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	a := &auth.Account{
		ID:           id,
		Email:        email,
		PasswordHash: h,
		Role:         role,
		Active:       true,
	}
	store.add(a)
	return a
}

// newBcryptPanelWithStore builds a Panel backed by BcryptTOTPAuth using store.
func newBcryptPanelWithStore(store auth.AccountStore) (*resource.Panel, *auth.BcryptTOTPAuth) {
	a := auth.NewBcryptTOTPAuth(auth.BcryptConfig{
		Store:      store,
		HMACKey:    []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:   "/admin",
		SessionTTL: time.Hour,
		Secure:     false,
	})
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	return p, a
}

// bcryptLogin logs in via POST /admin/login and returns the session cookie.
func bcryptLogin(t *testing.T, a *auth.BcryptTOTPAuth, email, pw string) *http.Cookie {
	t.Helper()
	body := strings.NewReader("email=" + email + "&password=" + pw)
	req := httptest.NewRequest(http.MethodPost, "/admin/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("bcryptLogin: expected 303, got %d body=%s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "panel_admin" {
			return c
		}
	}
	t.Fatal("bcryptLogin: no panel_admin cookie in response")
	return nil
}

// navResources returns a pair of resources: one open, one role-gated.
func openResource(name string) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: strings.ToUpper(name[:1]) + name[1:],
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Scope:  tenant.Scope{},
		Perms:  resource.ReadAny,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
	}
}

func gatedResource(name, requiredRole string) resource.Resource {
	r := openResource(name)
	r.RequiredRole = requiredRole
	return r
}

// ── Phase t3-3: nav-filter via Visible ──────────────────────────────────────

// TestNavFilter_Visible_FalseDropsItem verifies that a Resource with Visible
// returning false is absent from the rendered sidebar, while a Resource with
// nil Visible is present (zero-value safe, backward-compat).
//
// Red-on-revert: removing the Visible filter in navItemsFor causes the "gated"
// label to appear in the HTML, which fails the negative assertion.
func TestNavFilter_Visible_FalseDropsItem(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin")
	p, a := newBcryptPanelWithStore(store)

	// Open resource — always visible.
	resource.Register(p, openResource("open"))

	// Gated resource — Visible always returns false (simulates feature-flag off).
	hidden := openResource("hidden")
	hidden.Visible = func(_ context.Context) bool { return false }
	resource.Register(p, hidden)

	cookie := bcryptLogin(t, a, "op@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/open", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	// Open resource nav link MUST be present.
	if !strings.Contains(body, "Open") {
		t.Errorf("expected 'Open' nav label in sidebar, not found")
	}
	// Hidden resource nav link MUST be absent from the sidebar.
	if strings.Contains(body, "Hidden") {
		t.Errorf("expected 'Hidden' nav label to be absent from sidebar (Visible=false), but found it")
	}
}

// TestNavFilter_Visible_NilIsVisible verifies backward-compat: nil Visible
// renders the item (default open state, as before Phase t3-3).
func TestNavFilter_Visible_NilIsVisible(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "op@example.com", "s3cret", "admin")
	p, a := newBcryptPanelWithStore(store)

	resource.Register(p, openResource("always"))

	cookie := bcryptLogin(t, a, "op@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/always", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Always") {
		t.Errorf("expected 'Always' nav label in sidebar for nil Visible")
	}
}

// ── Phase t3-3: nav-filter via RequiredRole ──────────────────────────────────

// TestNavHide_DerivedFromRequiredRole_WrongRole verifies that a nav item with
// RequiredRole:"admin" is absent from the sidebar for a "support" session,
// even though a "support" operator can successfully authenticate.
//
// Cross-check: the route still returns 403 (route gate is RequiredRole, not Visible).
// Red-on-revert: removing the RequiredRole check in navItemsFor shows "Billing" in
// the sidebar for a "support" session, failing the negative assertion.
func TestNavHide_DerivedFromRequiredRole_WrongRole(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "support@example.com", "s3cret", "support")
	seedAccount(t, store, "u2", "admin@example.com", "s3cret", "admin")
	p, a := newBcryptPanelWithStore(store)

	resource.Register(p, openResource("dashboard"))
	resource.Register(p, gatedResource("billing", "admin"))

	supportCookie := bcryptLogin(t, a, "support@example.com", "s3cret")

	// Get the dashboard page (accessible) — check the sidebar rendered for support.
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(supportCookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("support: expected 200 for /admin/dashboard, got %d", w.Code)
	}
	body := w.Body.String()

	// Dashboard nav link is visible to support.
	if !strings.Contains(body, "Dashboard") {
		t.Errorf("support: expected 'Dashboard' nav label in sidebar")
	}
	// Billing nav link must NOT be visible to support.
	if strings.Contains(body, "Billing") {
		t.Errorf("support: expected 'Billing' nav label ABSENT from sidebar for support role, found it")
	}

	// Route gate: direct URL to billing must return 403 for support (not just nav-hidden).
	reqBilling := httptest.NewRequest(http.MethodGet, "/admin/billing", nil)
	reqBilling.AddCookie(supportCookie)
	wBilling := httptest.NewRecorder()
	p.Handler().ServeHTTP(wBilling, reqBilling)
	if wBilling.Code != http.StatusForbidden {
		t.Errorf("support: expected 403 for /admin/billing by direct URL, got %d", wBilling.Code)
	}
}

// TestNavHide_DerivedFromRequiredRole_MatchingRole verifies that a RequiredRole
// nav item IS present in the sidebar for a session whose role matches.
func TestNavHide_DerivedFromRequiredRole_MatchingRole(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "admin@example.com", "s3cret", "admin")
	p, a := newBcryptPanelWithStore(store)

	resource.Register(p, openResource("dashboard"))
	resource.Register(p, gatedResource("billing", "admin"))

	adminCookie := bcryptLogin(t, a, "admin@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("admin: expected 200 for /admin/dashboard, got %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, "Billing") {
		t.Errorf("admin: expected 'Billing' nav label in sidebar for admin role")
	}
}

// TestNavHide_DerivedFromRequiredRole_OwnerPassesAll verifies that the "owner"
// super-role sees all role-gated items regardless of RequiredRole value.
func TestNavHide_DerivedFromRequiredRole_OwnerPassesAll(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "owner@example.com", "s3cret", "owner")
	p, a := newBcryptPanelWithStore(store)

	resource.Register(p, openResource("dashboard"))
	resource.Register(p, gatedResource("billing", "admin"))
	resource.Register(p, gatedResource("system", "superadmin"))

	ownerCookie := bcryptLogin(t, a, "owner@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(ownerCookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("owner: expected 200 for /admin/dashboard, got %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, "Billing") {
		t.Errorf("owner: expected 'Billing' nav label in sidebar")
	}
	if !strings.Contains(body, "System") {
		t.Errorf("owner: expected 'System' nav label in sidebar")
	}
}

// ── handleIndex redirect fix ─────────────────────────────────────────────────

// TestHandleIndex_SkipsGatedResource verifies that GET /admin/ redirects to
// the first ACCESSIBLE resource, not the first registered resource. When the
// first registered resource is role-gated and the operator lacks that role,
// the redirect must target the next accessible one — not the gated one (which
// would result in an immediate 403 at the destination).
//
// Red-on-revert: reverting the navItemsFor call in handleIndex (using p.nav
// instead) causes the redirect to target /admin/billing, which the support
// operator cannot access (403 on arrival), failing the redirect assertion.
func TestHandleIndex_SkipsGatedResource(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "support@example.com", "s3cret", "support")
	p, a := newBcryptPanelWithStore(store)

	// Register gated first, open second — handleIndex must skip the gated one.
	resource.Register(p, gatedResource("billing", "admin"))
	resource.Register(p, openResource("dashboard"))

	supportCookie := bcryptLogin(t, a, "support@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(supportCookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 from /admin/, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "/admin/billing" {
		t.Error("handleIndex redirected support to /admin/billing (gated resource); expected /admin/dashboard")
	}
	if loc != "/admin/dashboard" {
		t.Errorf("expected redirect to /admin/dashboard, got %q", loc)
	}
}

// ── Phase t7: profile block ──────────────────────────────────────────────────

// TestProfileBlock_RendersNameAndRole verifies that when BcryptTOTPAuth sessions
// are active, the sidebar renders the profile block with the operator's name and role.
//
// Red-on-revert: removing the Profile overlay in chromeStateFrom (setting
// state.Profile = shell.ProfileConfig{}) causes Name/Role to remain empty,
// and the profile block is replaced by the bare Logout only, failing the
// name/role presence assertions.
func TestProfileBlock_RendersNameAndRole(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "alice@example.com", "s3cret", "admin")
	p, a := newBcryptPanelWithStore(store)
	resource.Register(p, openResource("things"))

	cookie := bcryptLogin(t, a, "alice@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/things", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	// class="sidebar-profile" appears only inside the <div> — not in the CSS
	// (which uses .sidebar-profile with a dot prefix). Presence confirms the
	// profile block branch ran (prof.Name != "").
	if !strings.Contains(body, `class="sidebar-profile"`) {
		t.Errorf("expected sidebar-profile block in rendered HTML; Profile.Name must have been populated from session")
	}
	// Name and role divs rendered inside the block.
	if !strings.Contains(body, `class="sidebar-profile-name"`) {
		t.Errorf("expected sidebar-profile-name element in profile block")
	}
	if !strings.Contains(body, `class="sidebar-profile-role"`) {
		t.Errorf("expected sidebar-profile-role element in profile block")
	}
}

// TestProfileBlock_DegradesToLogout_WhenNoSession verifies that when the
// authenticator is HMACAuth (no auth.SessionFrom session), the sidebar renders
// the bare Logout link, not the profile block — backward-compat for go-job.
//
// Red-on-revert: forcing Profile.Name="test" unconditionally in chromeStateFrom
// would show a profile block here, which would be wrong for HMACAuth consumers.
func TestProfileBlock_DegradesToLogout_WhenNoSession(t *testing.T) {
	// HMACAuth — does not store a session via auth.SessionFrom.
	p := newTestPanel() // uses HMACAuth
	resource.Register(p, openResource("things"))

	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	body := strings.NewReader("username=admin&password=secret")
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", body)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(loginW, loginReq)
	cookieVal := extractCookieValue(loginW.Header().Get("Set-Cookie"), "panel_admin")

	req := httptest.NewRequest(http.MethodGet, "/admin/things", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body2 := w.Body.String()

	// Bare Logout must be present.
	if !strings.Contains(body2, "Logout") {
		t.Errorf("expected bare Logout link for HMACAuth (no session), not found")
	}
	// Profile block div must NOT be present (no Name → bare mode).
	// Check for the HTML attribute form `class="sidebar-profile"` — the CSS
	// selector `.sidebar-profile` (with dot) also appears in the <style> block
	// and would cause false positives; the attribute form is unique to the HTML.
	if strings.Contains(body2, `class="sidebar-profile"`) {
		t.Errorf("expected no sidebar-profile block for HMACAuth consumer (no named session), but profile div found")
	}
}

// TestProfileBlock_SetProfile_StaticLogoutURL verifies that SetProfile
// configures the LogoutURL used in the profile block when a session is active.
func TestProfileBlock_SetProfile_StaticLogoutURL(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "alice@example.com", "s3cret", "admin")
	p, a := newBcryptPanelWithStore(store)
	p.SetProfile(shell.ProfileConfig{
		LogoutURL: "/custom/logout",
	})
	resource.Register(p, openResource("things"))

	cookie := bcryptLogin(t, a, "alice@example.com", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/admin/things", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "/custom/logout") {
		t.Errorf("expected custom logout URL /custom/logout in rendered sidebar")
	}
}

// ── Security follow-up: role-denied logging ──────────────────────────────────

// TestRoleDenied_403_OnDirectURL is an integration test that the route gate
// returns 403 for a session whose role does not match RequiredRole.
// The slog warning emitted on denial is a side effect observable in prod logs;
// the 403 status is the falsifiable assertion here.
//
// Red-on-revert: removing the role check from RequireRole (passing all sessions)
// would return 200 instead of 403, failing this test.
func TestRoleDenied_403_OnDirectURL(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "support@example.com", "s3cret", "support")
	p, a := newBcryptPanelWithStore(store)

	resource.Register(p, gatedResource("billing", "admin"))

	supportCookie := bcryptLogin(t, a, "support@example.com", "s3cret")

	for _, path := range []string{
		"/admin/billing",
		"/admin/billing/rows",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(supportCookie)
			w := httptest.NewRecorder()
			p.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("expected 403 for %s with support role, got %d", path, w.Code)
			}
		})
	}

	// Owner passes.
	seedAccount(t, store, "u2", "owner@example.com", "s3cret", "owner")
	ownerCookie := bcryptLogin(t, a, "owner@example.com", "s3cret")
	req := httptest.NewRequest(http.MethodGet, "/admin/billing", nil)
	req.AddCookie(ownerCookie)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin/billing with owner role, got %d", w.Code)
	}
}
