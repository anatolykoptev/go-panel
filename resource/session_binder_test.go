package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// headerAuth models a cookieless authenticator: identity arrives in a request
// header set by an upstream proxy, so there is no panel session cookie. Its
// gate lets every request through; the CSRF binding is the only thing under
// test. It implements SessionBinder, not SessionCookieName.
type headerAuth struct{}

func (headerAuth) Require(h http.HandlerFunc) http.HandlerFunc { return h }
func (headerAuth) LoginHandler() http.Handler                  { return http.NotFoundHandler() }
func (headerAuth) LogoutHandler() http.Handler                 { return http.NotFoundHandler() }
func (headerAuth) SessionBinding(r *http.Request) string       { return r.Header.Get("X-Session") }

// absentCookieAuth names a session cookie that clients never send, so every
// request has an empty binding.
type absentCookieAuth struct{ headerAuth }

func (absentCookieAuth) SessionCookieName() string { return "no_such_cookie" }

// bothAuth implements both seams; SessionBinder must win.
type bothAuth struct{ headerAuth }

func (bothAuth) SessionCookieName() string { return "panel_admin" }

// cookieOnlyAuth implements only SessionCookieName (the legacy seam).
type cookieOnlyAuth struct{}

func (cookieOnlyAuth) Require(h http.HandlerFunc) http.HandlerFunc { return h }
func (cookieOnlyAuth) LoginHandler() http.Handler                  { return http.NotFoundHandler() }
func (cookieOnlyAuth) LogoutHandler() http.Handler                 { return http.NotFoundHandler() }
func (cookieOnlyAuth) SessionCookieName() string                   { return "no_such_cookie" }

type panelAuth interface {
	Require(http.HandlerFunc) http.HandlerFunc
	LoginHandler() http.Handler
	LogoutHandler() http.Handler
}

// binderPanel mounts one MountAction route behind a, counting handler runs.
func binderPanel(a panelAuth, ran *int) *resource.Panel {
	p := resource.New(resource.Config{Title: "T", BasePath: "/admin", Auth: a, CSRFKey: testCSRFKey})
	p.MountAction(resource.ActionSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { *ran++; w.WriteHeader(http.StatusOK) },
	})
	return p
}

func postAction(p *resource.Panel, session, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/widget",
		strings.NewReader(url.Values{"_csrf": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if session != "" {
		req.Header.Set("X-Session", session)
	}
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// A token minted under binding A verifies under A and never under B.
func TestSessionBinder_TokenBoundToBinding(t *testing.T) {
	var ran int
	p := binderPanel(headerAuth{}, &ran)
	issue := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/x", nil)
	issue.Header.Set("X-Session", "A")
	tok := p.CSRFToken(issue)
	if tok == "" {
		t.Fatal("CSRFToken returned empty for a request with a binding")
	}
	if w := postAction(p, "A", tok); w.Code != http.StatusOK {
		t.Fatalf("binding A: want 200, got %d", w.Code)
	}
	if w := postAction(p, "B", tok); w.Code != http.StatusForbidden {
		t.Fatalf("token minted for A posted under B: want 403, got %d", w.Code)
	}
	if ran != 1 {
		t.Fatalf("handler ran %d times, want 1", ran)
	}
}

// A token minted over the empty binding must not verify on a request that has
// none. Falsification: drop the empty-binding reject in verifyCSRFToken.
func TestSessionBinder_EmptyBindingRejected(t *testing.T) {
	for name, a := range map[string]panelAuth{
		"binder":        headerAuth{},
		"absent cookie": absentCookieAuth{},
		"cookie only":   cookieOnlyAuth{},
	} {
		t.Run(name, func(t *testing.T) {
			var ran int
			p := binderPanel(a, &ran)
			forged := csrf.Issue(testCSRFKey, "", csrf.DefaultTTL)
			w := postAction(p, "", forged)
			if w.Code != http.StatusForbidden {
				t.Fatalf("token over empty binding accepted: want 403, got %d", w.Code)
			}
			if ran != 0 {
				t.Fatalf("handler ran %d times behind an empty binding, want 0", ran)
			}
		})
	}
}

// No binding means no token is issued: the form renders dead rather than bound
// to the empty string.
func TestSessionBinder_NoTokenIssuedWithoutBinding(t *testing.T) {
	p := binderPanel(headerAuth{}, new(int))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/x", nil)
	if tok := p.CSRFToken(req); tok != "" {
		t.Fatalf("CSRFToken issued %q for a request with no binding, want empty", tok)
	}
}

// SessionBinder takes precedence over SessionCookieName.
func TestSessionBinder_PreferredOverCookieName(t *testing.T) {
	var ran int
	p := binderPanel(bothAuth{}, &ran)
	issue := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/x", nil)
	issue.Header.Set("X-Session", "A")
	issue.AddCookie(&http.Cookie{Name: "panel_admin", Value: "cookie-value"})
	tok := p.CSRFToken(issue)
	if w := postAction(p, "A", tok); w.Code != http.StatusOK {
		t.Fatalf("token bound via SessionBinding: want 200, got %d", w.Code)
	}
	if ran != 1 {
		t.Fatalf("handler ran %d times, want 1", ran)
	}
}

// The Writer save route enforces the same binding: a token minted under A is
// refused under B and Save never runs.
func TestSessionBinder_WriterSaveRejectsOtherBinding(t *testing.T) {
	p := resource.New(resource.Config{Title: "T", BasePath: "/admin", Auth: headerAuth{}, CSRFKey: testCSRFKey})
	var saves int
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) { return map[string]string{}, nil },
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { saves++; return nil },
	))
	issue := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/new", nil)
	issue.Header.Set("X-Session", "A")
	tok := p.CSRFToken(issue)

	post := func(session string) int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
			strings.NewReader(url.Values{"name": {"x"}, "_csrf": {tok}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Session", session)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		return w.Code
	}
	if code := post("B"); code != http.StatusForbidden {
		t.Fatalf("save under binding B: want 403, got %d", code)
	}
	if saves != 0 {
		t.Fatalf("Save ran %d times under a foreign binding, want 0", saves)
	}
	if code := post("A"); code == http.StatusForbidden {
		t.Fatalf("save under binding A refused with 403")
	}
}
