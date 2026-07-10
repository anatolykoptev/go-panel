package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
)

// TestMountPage_IndexOverride verifies that a Path:"" page replaces the
// default index redirect, and stays guarded exactly like the default index.
func TestMountPage_IndexOverride(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)

	var ran bool
	p.MountPage(resource.PageSpec{
		Path: "",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			ran = true
			_, _ = w.Write([]byte("custom-index-body"))
		},
	})

	t.Run("authed GET /admin/ serves the custom page, not a resource redirect", func(t *testing.T) {
		ran = false
		cookieVal := authCookie(t, a, "admin", "secret")
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !ran {
			t.Error("custom index handler did not run")
		}
		if got := w.Body.String(); got != "custom-index-body" {
			t.Errorf("expected custom-index-body, got %q", got)
		}
	})

	t.Run("anon GET /admin/ redirects to login, custom handler never runs", func(t *testing.T) {
		ran = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/admin/login" {
			t.Errorf("expected Location /admin/login, got %q", loc)
		}
		if ran {
			t.Error("custom index handler ran on an unauthenticated request — guard was bypassed")
		}
	})
}

// TestMountPage_SubPage verifies a non-index PageSpec mounts at
// {basePath}/{Path}/{$}, guarded, with the standard net/http mux redirect
// and 404/405 behaviour around it.
func TestMountPage_SubPage(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)

	p.MountPage(resource.PageSpec{
		Path: "report",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("report-body"))
		},
	})

	cookieVal := authCookie(t, a, "admin", "secret")

	t.Run("authed GET /admin/report/ -> 200 body", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/report/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if got := w.Body.String(); got != "report-body" {
			t.Errorf("expected report-body, got %q", got)
		}
	})

	t.Run("anon GET /admin/report/ -> 303 login (guard)", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/report/", nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/admin/login" {
			t.Errorf("expected Location /admin/login, got %q", loc)
		}
	})

	t.Run("GET /admin/report (no slash) -> 307 to /admin/report/", func(t *testing.T) {
		// go1.26 net/http.ServeMux issues an implicit redirect for the missing
		// trailing slash on a "{$}"-terminated pattern. It is a 307 Temporary
		// Redirect, NOT the classic 301 — functionally equivalent (nav always
		// links the slash form, so this only fires on a hand-typed URL) and
		// arguably safer (307 preserves method/body across the redirect hop,
		// where 301 traditionally invited clients to downgrade to GET).
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/report", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusTemporaryRedirect {
			t.Fatalf("expected 307, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/admin/report/" {
			t.Errorf("expected Location /admin/report/, got %q", loc)
		}
	})

	t.Run("GET /admin/report/deeper -> 404", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/report/deeper", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("POST /admin/report/ -> 405", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/report/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
	})
}

// TestMountPage_Alias verifies that Aliases mount exact-match GET suffixes
// serving the same guarded handler as the page they're attached to.
func TestMountPage_Alias(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)

	p.MountPage(resource.PageSpec{
		Path: "",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("index-body"))
		},
		Aliases: []string{"overview"},
	})

	cookieVal := authCookie(t, a, "admin", "secret")

	t.Run("authed GET /admin/ and /admin/overview both serve the same body", func(t *testing.T) {
		for _, path := range []string{"/admin/", "/admin/overview"} {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
			w := httptest.NewRecorder()
			p.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d", path, w.Code)
			}
			if got := w.Body.String(); got != "index-body" {
				t.Errorf("%s: expected index-body, got %q", path, got)
			}
		}
	})

	t.Run("anon GET /admin/overview -> 303 login (alias is guarded too)", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/overview", nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/admin/login" {
			t.Errorf("expected Location /admin/login, got %q", loc)
		}
	})
}

// TestMountPage_Panics covers the fail-closed misuse cases: MountPage after
// finalization, a nil Handler, a duplicate index, an empty alias, and a role
// the configured authenticator cannot back.
func TestMountPage_Panics(t *testing.T) {
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

	t.Run("MountPage after Handler() panics", func(t *testing.T) {
		p := panelWithAuth(newTestAuth())
		p.Handler() // finalizes the mux

		assertPanics(t, func() {
			p.MountPage(resource.PageSpec{Path: "late", Handler: noopHandler})
		})
	})

	t.Run("nil Handler panics", func(t *testing.T) {
		p := panelWithAuth(newTestAuth())

		assertPanics(t, func() {
			p.MountPage(resource.PageSpec{Path: "x"})
		})
	})

	t.Run("second Path colon empty string panics", func(t *testing.T) {
		p := panelWithAuth(newTestAuth())
		p.MountPage(resource.PageSpec{Path: "", Handler: noopHandler})

		assertPanics(t, func() {
			p.MountPage(resource.PageSpec{Path: "", Handler: noopHandler})
		})
	})

	t.Run("empty alias panics", func(t *testing.T) {
		p := panelWithAuth(newTestAuth())

		assertPanics(t, func() {
			p.MountPage(resource.PageSpec{
				Path:    "sub",
				Handler: noopHandler,
				Aliases: []string{"/"}, // trims to "" -> panic
			})
		})
	})

	t.Run("RequiredRole on a non-RoleAuthenticator panics", func(t *testing.T) {
		// newTestAuth() returns *auth.HMACAuth, which does not implement
		// RoleAuthenticator — guard() must fail closed at mount time.
		p := panelWithAuth(newTestAuth())

		assertPanics(t, func() {
			p.MountPage(resource.PageSpec{
				Path:         "billing",
				Handler:      noopHandler,
				RequiredRole: "admin",
			})
		})
	})
}
