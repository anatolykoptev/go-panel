package resource_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/shell"
)

// TestRenderPage_ContainsChrome verifies that RenderPage wraps content in the
// full panel chrome: sidebar, title in <head>, and the supplied content.
func TestRenderPage_ContainsChrome(t *testing.T) {
	p := newTestPanel()
	p.AddNav(shell.NavItem{
		ID:    "my-page",
		Label: "My Page",
		URL:   "/admin/my-page",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/my-page", nil)

	content := templ.Raw("<p>hello from content</p>")
	if err := p.RenderPage(w, r, "Test Panel", "my-page", content); err != nil {
		t.Fatalf("RenderPage returned error: %v", err)
	}

	resp := w.Body.String()

	// Sidebar present.
	if !strings.Contains(resp, `class="sidebar"`) {
		t.Error("response does not contain sidebar element")
	}
	// Title present.
	if !strings.Contains(resp, "Test Panel") {
		t.Error("response does not contain the page title")
	}
	// Content present.
	if !strings.Contains(resp, "hello from content") {
		t.Error("response does not contain the supplied content")
	}
	// Content-Type set.
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html; charset=utf-8, got %q", ct)
	}
}

// TestRenderPage_ActiveNav verifies that when activeID matches a registered nav
// item, that item's anchor element contains the active CSS classes. The layout
// renders active items as: class="sidebar-item pm7-sidebar-item active pm7-sidebar-item--active"
func TestRenderPage_ActiveNav(t *testing.T) {
	p := newTestPanel()
	p.AddNav(shell.NavItem{
		ID:    "dashboard",
		Label: "Dashboard",
		URL:   "/admin/dashboard",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)

	if err := p.RenderPage(w, r, "Admin", "dashboard", templ.Raw("")); err != nil {
		t.Fatalf("RenderPage returned error: %v", err)
	}

	resp := w.Body.String()

	// The layout renders active items with "active pm7-sidebar-item--active" appended
	// to the anchor's class attribute.
	if !strings.Contains(resp, `pm7-sidebar-item active pm7-sidebar-item--active"`) {
		t.Error("response does not contain active nav classes on the Dashboard anchor element")
	}
}

// TestRenderPage_NoActiveNav verifies that when activeID is "", no nav item's
// anchor element is marked active.
func TestRenderPage_NoActiveNav(t *testing.T) {
	p := newTestPanel()
	p.AddNav(shell.NavItem{
		ID:    "dashboard",
		Label: "Dashboard",
		URL:   "/admin/dashboard",
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/other", nil)

	if err := p.RenderPage(w, r, "Admin", "", templ.Raw("")); err != nil {
		t.Fatalf("RenderPage returned error: %v", err)
	}

	resp := w.Body.String()

	// Nav item present.
	if !strings.Contains(resp, "Dashboard") {
		t.Error("response does not contain the Dashboard nav label")
	}
	// The anchor element must NOT carry the active classes.
	// (The CSS stylesheet contains pm7-sidebar-item--active as a rule name, but the
	// element-level class string includes the preceding "active" only when item.Active=true.)
	if strings.Contains(resp, `pm7-sidebar-item active pm7-sidebar-item--active"`) {
		t.Error("response should not contain active nav classes on any anchor when activeID is empty")
	}
}

// TestRenderPageHTML_ContentPassthrough verifies that RenderPageHTML passes
// pre-escaped HTML verbatim into the response and sets the correct Content-Type.
func TestRenderPageHTML_ContentPassthrough(t *testing.T) {
	p := newTestPanel()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/custom", nil)

	rawHTML := "<p>hello &amp; world</p>"
	if err := p.RenderPageHTML(w, r, "Admin", "", rawHTML); err != nil {
		t.Fatalf("RenderPageHTML returned error: %v", err)
	}

	resp := w.Body.String()

	if !strings.Contains(resp, rawHTML) {
		t.Errorf("response does not contain the raw HTML verbatim; got body length %d", len(resp))
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html; charset=utf-8, got %q", ct)
	}
}

// TestRenderPage_SetsSecurityHeaders verifies that RenderPage sets the standard
// admin security headers (CSP non-empty + X-Frame-Options: DENY) before writing
// the body. Bespoke pages must have the same security posture as resource pages.
// Falsification: remove the shell.SecurityHeaders(w) call → both assertions fail.
func TestRenderPage_SetsSecurityHeaders(t *testing.T) {
	p := newTestPanel()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/bespoke", nil)

	if err := p.RenderPage(w, r, "Admin", "", templ.Raw("<p>content</p>")); err != nil {
		t.Fatalf("RenderPage returned error: %v", err)
	}

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("RenderPage: Content-Security-Policy header is absent; shell.SecurityHeaders must be called")
	}
	xfo := w.Header().Get("X-Frame-Options")
	if xfo != "DENY" {
		t.Errorf("RenderPage: expected X-Frame-Options: DENY, got %q", xfo)
	}
}

// TestRenderPageHTML_SetsSecurityHeaders verifies that RenderPageHTML (which
// delegates to RenderPage) also inherits the security headers.
// Falsification: remove the shell.SecurityHeaders(w) call from RenderPage ->
// both assertions fail for RenderPageHTML too.
func TestRenderPageHTML_SetsSecurityHeaders(t *testing.T) {
	p := newTestPanel()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/bespoke-html", nil)

	if err := p.RenderPageHTML(w, r, "Admin", "", "<p>safe html</p>"); err != nil {
		t.Fatalf("RenderPageHTML returned error: %v", err)
	}

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("RenderPageHTML: Content-Security-Policy header is absent; RenderPage must call shell.SecurityHeaders")
	}
	xfo := w.Header().Get("X-Frame-Options")
	if xfo != "DENY" {
		t.Errorf("RenderPageHTML: expected X-Frame-Options: DENY, got %q", xfo)
	}
}
