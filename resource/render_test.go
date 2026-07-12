package resource_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
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

// TestRenderPage_CollapsedFromCookie verifies that RenderPage SSR-renders the
// sidebar with class="sidebar collapsed" when the request carries cookie sb-c=1.
// This kills the FOUC that occurs with localStorage-based collapse.
// Falsification: remove the ContextWithSidebar call in render.go →
// the aside stays class="sidebar" regardless of the cookie.
func TestRenderPage_CollapsedFromCookie(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: shell.SidebarCookie, Value: "1"})
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `class="sidebar collapsed"`) {
		t.Fatal("collapsed cookie did not SSR-render collapsed sidebar (FOUC not killed)")
	}
}

// TestRenderPage_ExpandedWithoutCookie verifies that without the sb-c cookie
// the aside renders as class="sidebar" (not collapsed) — baseline preserved.
func TestRenderPage_ExpandedWithoutCookie(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Contains(body, `class="sidebar collapsed"`) {
		t.Fatal("no cookie should yield expanded sidebar, not collapsed")
	}
	if !strings.Contains(body, `class="sidebar"`) {
		t.Fatal("expanded sidebar missing expected class=\"sidebar\"")
	}
}

// TestRenderError_Success verifies RenderError writes the buffered body in
// one shot on a successful render: Content-Type set, the panel chrome
// (built from the caller-supplied nav) and content both present.
func TestRenderError_Success(t *testing.T) {
	p := newTestPanel()
	w := httptest.NewRecorder()
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x"}}

	p.RenderError(context.Background(), w, "Admin", nav, templ.Raw("<p>hello from RenderError</p>"), "test-label", "render failed")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on a successful render, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "hello from RenderError") {
		t.Error("response does not contain the supplied content")
	}
	if !strings.Contains(body, `class="sidebar"`) {
		t.Error("response does not contain the panel chrome (shell.Layout)")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html; charset=utf-8, got %q", ct)
	}
}

// TestRenderError_UsesProvidedNav verifies RenderError renders EXACTLY the
// nav slice passed in, not p's own configured/registered nav — faithful to
// go-grad's cabinet/overview.go renderLayoutPage, which takes nav as a
// parameter (its caller computes nav independently via its own closure).
// Falsification: swap the nav argument for p.navItemsFor(ctx, "") internally
// -> this nav item (never registered via p.AddNav) disappears from the
// response.
func TestRenderError_UsesProvidedNav(t *testing.T) {
	p := newTestPanel() // deliberately no AddNav calls
	w := httptest.NewRecorder()
	nav := []shell.NavItem{{ID: "caller-only", Label: "Caller-Only-Nav-Item", URL: "/admin/x"}}

	p.RenderError(context.Background(), w, "Admin", nav, templ.Raw(""), "test-label", "render failed")

	if !strings.Contains(w.Body.String(), "Caller-Only-Nav-Item") {
		t.Error("expected the caller-supplied nav item to render — RenderError must not substitute its own nav computation")
	}
}

// TestRenderError_FailureLogsAndWritesGenericMessage verifies the extraction
// of go-grad's cabinet/overview.go renderLayoutPage error branch: a Render
// failure is logged server-side (label + err detail) and the client
// receives ONLY the caller-supplied message at 500 — never err's detail
// (no internal detail leak), and never any partial body a direct
// (unbuffered) render would already have flushed before the error surfaced.
// Falsification: render content directly to w instead of buffering first ->
// the PARTIAL-CONTENT-LEAK marker (written by failingContent before it
// errors) would appear in w.Body ahead of/alongside the 500 body.
func TestRenderError_FailureLogsAndWritesGenericMessage(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	p := newTestPanel()
	w := httptest.NewRecorder()
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x"}}

	const secretDetail = "pgx: connection refused on host 10.0.0.9"
	failingContent := templ.ComponentFunc(func(_ context.Context, cw io.Writer) error {
		_, _ = io.WriteString(cw, "PARTIAL-CONTENT-LEAK")
		return errors.New(secretDetail)
	})

	p.RenderError(context.Background(), w, "Admin", nav, failingContent, "overview-render", "Ошибка отрисовки страницы.")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	respBody := w.Body.String()
	if strings.TrimSpace(respBody) != "Ошибка отрисовки страницы." {
		t.Errorf("expected the exact caller-supplied message, got %q", respBody)
	}
	if strings.Contains(respBody, "PARTIAL-CONTENT-LEAK") {
		t.Error("response contains partially-rendered content — RenderError must buffer, never write before Render succeeds")
	}
	if strings.Contains(respBody, secretDetail) {
		t.Error("response leaks the internal render error detail to the client")
	}
	logOut := logBuf.String()
	if !strings.Contains(logOut, "overview-render") {
		t.Error("server-side log is missing the label")
	}
	if !strings.Contains(logOut, secretDetail) {
		t.Error("server-side log is missing the render error detail")
	}
}
