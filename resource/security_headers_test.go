package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

// wantCSP pins the full policy as a literal, independent of
// shell.ContentSecurityPolicy, so loosening any directive fails here.
const wantCSP = "default-src 'self'; img-src 'self' data:; script-src 'self' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; font-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'self'; form-action 'self'; object-src 'none'"

func assertSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder, wantCache string) {
	t.Helper()
	h := w.Header()
	if got := h.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
	}
	if got := h.Get("Cache-Control"); got != wantCache {
		t.Errorf("Cache-Control = %q, want %q", got, wantCache)
	}
}

// The CSP constant is the literal policy: pins the value so a loosened
// script-src (or a dropped directive) turns this RED.
func TestContentSecurityPolicy_PinnedValue(t *testing.T) {
	if shell.ContentSecurityPolicy != wantCSP {
		t.Fatalf("shell.ContentSecurityPolicy = %q, want %q", shell.ContentSecurityPolicy, wantCSP)
	}
	w := httptest.NewRecorder()
	shell.SecurityHeaders(w)
	if got := w.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("SecurityHeaders CSP = %q, want %q", got, wantCSP)
	}
	shell.SecurityHeaders(w) // idempotent: Set, never Add
	if n := len(w.Header().Values("Content-Security-Policy")); n != 1 {
		t.Fatalf("CSP header repeated %d times after two calls, want 1", n)
	}
}

// Every response class served through Panel.Handler carries the headers:
// login page, a rendered list, a MountPage handler that never calls
// RenderPage, and a static asset (which keeps its own Cache-Control).
// Falsification: remove withSecurityHeaders from Handler() -> login, bare
// MountPage and static cases go RED.
func TestPanelHandler_SecurityHeadersOnEveryRouteClass(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, groupResource("places", "Content"))
	p.MountPage(resource.PageSpec{
		Path: "bare",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("bare")) // never calls RenderPage / SecurityHeaders
		},
	})
	h := p.Handler()
	cookieVal, _ := loginAndGetCookie(t, p)

	get := func(path string, withSession bool) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		if withSession {
			req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	cases := []struct {
		name, path string
		session    bool
		wantCode   int
		wantCache  string
	}{
		{"login page", "/admin/login", false, http.StatusOK, "no-store"},
		{"list", "/admin/places", true, http.StatusOK, "no-store"},
		{"bare MountPage", "/admin/bare/", true, http.StatusOK, "no-store"},
		{"static asset", "/admin/static/admin.js", false, http.StatusOK, "public, max-age=86400"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := get(tc.path, tc.session)
			if w.Code != tc.wantCode {
				t.Fatalf("GET %s = %d, want %d", tc.path, w.Code, tc.wantCode)
			}
			assertSecurityHeaders(t, w, tc.wantCache)
		})
	}
}
