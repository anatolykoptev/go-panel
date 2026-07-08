package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// authCookie returns a valid session cookie value by performing a real login
// against the given auth instance.
func authCookie(t *testing.T, a *auth.HMACAuth, username, password string) string {
	t.Helper()
	body := strings.NewReader("username=" + username + "&password=" + password)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, req)
	v := extractCookieValue(w.Header().Get("Set-Cookie"), "panel_admin")
	if v == "" {
		t.Fatal("authCookie: login did not set panel_admin cookie")
	}
	return v
}

// minimalResource builds a valid Resource with the given name for use in tests.
func minimalResource(name string) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: strings.ToUpper(name[:1]) + name[1:],
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
	}
}

// groupResource is like minimalResource but with a Group set (causes a group-header nav item).
func groupResource(name, group string) resource.Resource {
	r := minimalResource(name)
	r.Group = group
	return r
}

func newTestAuth() *auth.HMACAuth {
	return auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
}

func panelWithAuth(a *auth.HMACAuth) *resource.Panel {
	return resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
}

func TestIndexRoute(t *testing.T) {
	t.Run("anon GET /admin/ redirects to login", func(t *testing.T) {
		a := newTestAuth()
		p := panelWithAuth(a)
		// Register a resource so there's something to land on.
		resource.Register(p, groupResource("places", "Content"))

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if loc != "/admin/login" {
			t.Errorf("expected Location /admin/login, got %q", loc)
		}
	})

	t.Run("anon GET /admin/ with HX-Request returns 401 + HX-Redirect", func(t *testing.T) {
		a := newTestAuth()
		p := panelWithAuth(a)
		resource.Register(p, minimalResource("places"))

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		hxRedir := w.Header().Get("HX-Redirect")
		if hxRedir != "/admin/login" {
			t.Errorf("expected HX-Redirect /admin/login, got %q", hxRedir)
		}
	})

	t.Run("auth GET /admin/ with resources redirects to first real resource, skipping group header", func(t *testing.T) {
		a := newTestAuth()
		p := panelWithAuth(a)
		// Register group-header resource first, then a real one.
		// groupResource adds a group-header nav item (empty URL) before the real nav item.
		resource.Register(p, groupResource("places", "Content"))
		resource.Register(p, groupResource("events", "Content")) // same group, no dup header

		cookieVal := authCookie(t, a, "admin", "secret")

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		// First real resource (not group header) is "places"
		if loc != "/admin/places" {
			t.Errorf("expected Location /admin/places, got %q", loc)
		}
	})

	t.Run("auth GET /admin/ with 0 resources returns 200 minimal page", func(t *testing.T) {
		a := newTestAuth()
		p := panelWithAuth(a) // no Register calls

		cookieVal := authCookie(t, a, "admin", "secret")

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "Admin") {
			t.Errorf("expected 'Admin' in minimal page body, got: %q", body)
		}
	})

	t.Run("anon GET /admin/login returns 200 — no redirect loop", func(t *testing.T) {
		a := newTestAuth()
		p := panelWithAuth(a)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/login", nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("auth GET /admin/places is reachable", func(t *testing.T) {
		a := newTestAuth()
		p := panelWithAuth(a)
		resource.Register(p, minimalResource("places"))

		cookieVal := authCookie(t, a, "admin", "secret")

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/places", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code == http.StatusNotFound {
			t.Fatalf("expected non-404 for /admin/places, got 404")
		}
		// 200 expected (lister returns empty, page still renders)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}
