package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
)

// actionPostRequest builds a POST request to path carrying form as its
// url-encoded body, with cookieVal attached as the panel_admin session
// cookie (empty cookieVal = no cookie, i.e. anonymous).
func actionPostRequest(path, cookieVal string, form url.Values) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookieVal != "" {
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	}
	return req
}

// TestMountAction_ValidCSRF_ReachesHandler verifies a POST carrying a valid,
// session-bound CSRF token reaches Handler with the path wildcard resolved
// and the form already parsed — proving MountAction's pre-flight (CSRF
// verify + ParseForm) ran before Handler, not after.
// Falsification: delete the ParseForm/FormValue plumbing in csrfProtect ->
// gotNote reads empty even though the field was submitted.
func TestMountAction_ValidCSRF_ReachesHandler(t *testing.T) {
	var ran bool
	var gotID, gotNote string
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path: "widget/{id}/note",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			ran = true
			gotID = r.PathValue("id")
			gotNote = r.FormValue("note")
			w.WriteHeader(http.StatusOK)
		},
	})
	cookieVal, _ := loginAndGetCookie(t, p)
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)

	req := actionPostRequest("/admin/widget/42/note", cookieVal, url.Values{
		"_csrf": {tok},
		"note":  {"hello"},
	})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !ran {
		t.Fatal("handler did not run")
	}
	if gotID != "42" {
		t.Errorf("expected PathValue(id)=42, got %q", gotID)
	}
	if gotNote != "hello" {
		t.Errorf("expected FormValue(note)=hello (proves ParseForm ran before Handler), got %q", gotNote)
	}
}

// TestMountAction_MissingCSRF_Rejected verifies a POST with no _csrf field
// at all is rejected 403 before Handler runs.
// Falsification: remove the csrf.Verify call from csrfProtect -> this test
// fails (200, ran=true).
func TestMountAction_MissingCSRF_Rejected(t *testing.T) {
	var ran bool
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal, _ := loginAndGetCookie(t, p)

	req := actionPostRequest("/admin/widget", cookieVal, url.Values{})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	if ran {
		t.Error("handler ran despite a missing CSRF token")
	}
}

// TestMountAction_InvalidCSRF_Rejected verifies a garbage _csrf value is
// rejected 403 before Handler runs.
func TestMountAction_InvalidCSRF_Rejected(t *testing.T) {
	var ran bool
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal, _ := loginAndGetCookie(t, p)

	req := actionPostRequest("/admin/widget", cookieVal, url.Values{"_csrf": {"not-a-real-token"}})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	if ran {
		t.Error("handler ran despite an invalid CSRF token")
	}
}

// TestMountAction_WrongSessionCSRF_Rejected verifies a well-formed, validly
// signed CSRF token issued for a DIFFERENT session is still rejected —
// proving double-submit session BINDING is enforced, not just signature
// validity.
func TestMountAction_WrongSessionCSRF_Rejected(t *testing.T) {
	var ran bool
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	sessionA, _ := loginAndGetCookie(t, p)
	sessionB, _ := loginAndGetCookie(t, p)
	if sessionA == sessionB {
		t.Fatal("test setup: two logins produced the same session cookie value, can't test binding")
	}
	tokForA := csrf.Issue(testCSRFKey, sessionA, csrf.DefaultTTL)

	req := actionPostRequest("/admin/widget", sessionB, url.Values{"_csrf": {tokForA}})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	if ran {
		t.Error("handler ran despite a CSRF token bound to a different session")
	}
}

// TestMountAction_GET_MethodNotAllowed verifies MountAction mounts POST
// only — a GET to the same path 405s and never reaches Handler.
func TestMountAction_GET_MethodNotAllowed(t *testing.T) {
	var ran bool
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal, _ := loginAndGetCookie(t, p)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widget", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	if ran {
		t.Error("handler ran on a GET the mux should have rejected")
	}
}

// TestMountAction_AnonPOST_RedirectsToLogin verifies the auth guard runs
// before CSRF verification: an unauthenticated POST is redirected to login,
// never reaching the CSRF check or Handler.
func TestMountAction_AnonPOST_RedirectsToLogin(t *testing.T) {
	var ran bool
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})

	req := actionPostRequest("/admin/widget", "", url.Values{}) // no cookie, no csrf token
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect to login, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("expected Location /admin/login, got %q", loc)
	}
	if ran {
		t.Error("handler ran on an unauthenticated POST — guard was bypassed")
	}
}

// TestMountAction_RequiredRole verifies MountAction's RequiredRole wiring
// end-to-end: a session whose role matches reaches Handler; a session whose
// role does not is denied 403. The MountAction analogue of
// TestMountPage_RequiredRole (page_test.go), reusing the same
// BcryptTOTPAuth/AccountStore role harness from nav_filter_test.go.
func TestMountAction_RequiredRole(t *testing.T) {
	store := newTestAccountStore()
	seedAccount(t, store, "u1", "admin@example.com", "s3cret", "admin")
	seedAccount(t, store, "u2", "support@example.com", "s3cret", "support")
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
		CSRFKey:  testCSRFKey,
	})

	var ran bool
	p.MountAction(resource.ActionSpec{
		Path:         "billing/confirm",
		RequiredRole: "admin",
		Handler:      func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})

	t.Run("admin session reaches the handler", func(t *testing.T) {
		ran = false
		cookie := bcryptLogin(t, a, "admin@example.com", "s3cret")
		tok := csrf.Issue(testCSRFKey, cookie.Value, csrf.DefaultTTL)
		req := actionPostRequest("/admin/billing/confirm", cookie.Value, url.Values{"_csrf": {tok}})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if !ran {
			t.Error("handler did not run for the matching role")
		}
	})

	t.Run("wrong-role session is denied", func(t *testing.T) {
		ran = false
		cookie := bcryptLogin(t, a, "support@example.com", "s3cret")
		tok := csrf.Issue(testCSRFKey, cookie.Value, csrf.DefaultTTL)
		req := actionPostRequest("/admin/billing/confirm", cookie.Value, url.Values{"_csrf": {tok}})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
		if ran {
			t.Error("handler ran for a session with the wrong role")
		}
	})
}

// TestMountAction_BadFormBody_Rejected verifies a body exceeding
// MountAction's size cap fails ParseForm and is rejected 400, never
// reaching Handler.
func TestMountAction_BadFormBody_Rejected(t *testing.T) {
	var ran bool
	p := newWriterPanel()
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal, _ := loginAndGetCookie(t, p)

	oversized := strings.Repeat("a", (1<<20)+1024) // exceeds the 1 MB cap
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/widget", strings.NewReader(oversized))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if ran {
		t.Error("handler ran despite an oversized body")
	}
}

// TestMountAction_Panics covers the fail-closed misuse cases: MountAction
// after finalization, a nil Handler, an empty Path, a role the configured
// authenticator cannot back, a missing/short CSRFKey, and an authenticator
// that cannot bind CSRF to a session cookie. The last three are unique to
// MountAction (MountPage never touches CSRF, so it needs none of them) —
// see validateWriterConfig for the pre-existing analogue this mirrors.
func TestMountAction_Panics(t *testing.T) {
	noopHandler := func(http.ResponseWriter, *http.Request) {}

	assertPanics := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic, got none")
			}
		}()
		fn()
	}

	t.Run("MountAction after Handler() panics", func(t *testing.T) {
		p := newWriterPanel()
		p.Handler() // finalizes the mux
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "late", Handler: noopHandler})
		})
	})

	t.Run("nil Handler panics", func(t *testing.T) {
		p := newWriterPanel()
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "x"})
		})
	})

	t.Run("empty Path panics", func(t *testing.T) {
		p := newWriterPanel()
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "", Handler: noopHandler})
		})
	})

	t.Run("RequiredRole on a non-RoleAuthenticator panics", func(t *testing.T) {
		p := newWriterPanel() // HMACAuth does not implement RoleAuthenticator
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "billing", Handler: noopHandler, RequiredRole: "admin"})
		})
	})

	t.Run("empty CSRFKey panics", func(t *testing.T) {
		a := newTestAuth()
		p := resource.New(resource.Config{Title: "Test Panel", BasePath: "/admin", Auth: a})
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "widget", Handler: noopHandler})
		})
	})

	t.Run("short CSRFKey panics", func(t *testing.T) {
		a := newTestAuth()
		p := resource.New(resource.Config{Title: "Test Panel", BasePath: "/admin", Auth: a, CSRFKey: []byte("short")})
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "widget", Handler: noopHandler})
		})
	})

	t.Run("authenticator without SessionCookieName panics", func(t *testing.T) {
		// noSessionNameAuth is defined in writer_test.go (same package) —
		// the pre-existing stub validateWriterConfig's own analogous test
		// already uses for this exact capability gap.
		p := resource.New(resource.Config{Title: "Test Panel", BasePath: "/admin", Auth: noSessionNameAuth{}, CSRFKey: testCSRFKey})
		assertPanics(t, func() {
			p.MountAction(resource.ActionSpec{Path: "widget", Handler: noopHandler})
		})
	})
}
