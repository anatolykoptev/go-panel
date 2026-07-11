package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
)

// TestMountPage_Method_DefaultsToGET falsifies that PageSpec.Method's
// zero value keeps every pre-existing MountPage caller byte-identical: a
// POST to a Method:"" page must 405, never reach the handler.
func TestMountPage_Method_DefaultsToGET(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	var ran bool
	p.MountPage(resource.PageSpec{
		Path:    "widget",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/widget/", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to a Method:\"\" (GET-only) page: expected 405, got %d", w.Code)
	}
	if ran {
		t.Error("handler ran on a POST the mux should have rejected before the guard")
	}
}

// TestMountPage_Method_POST mounts a POST-only action page and verifies GET
// 405s (never reaches the handler) while POST reaches it, guarded.
func TestMountPage_Method_POST(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	var ran bool
	p.MountPage(resource.PageSpec{
		Path:    "confirm",
		Method:  http.MethodPost,
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal := authCookie(t, a, "admin", "secret")

	t.Run("GET 405s, handler never runs", func(t *testing.T) {
		ran = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/confirm/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", w.Code)
		}
		if ran {
			t.Error("handler ran on a GET the mux should have rejected")
		}
	})

	t.Run("authed POST reaches the guarded handler", func(t *testing.T) {
		ran = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/confirm/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK || !ran {
			t.Fatalf("expected 200 and handler run, got code=%d ran=%v", w.Code, ran)
		}
	})

	t.Run("anon POST is denied by the guard before the handler runs", func(t *testing.T) {
		ran = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/confirm/", nil)
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 redirect to login, got %d", w.Code)
		}
		if ran {
			t.Error("handler ran on an unauthenticated POST — guard was bypassed")
		}
	})
}

// TestMountPage_Method_GETAndPOSTSamePath verifies the documented pattern
// for a GET-form + POST-action pair sharing one Path: two MountPage calls,
// same Path, different Method, both reachable.
func TestMountPage_Method_GETAndPOSTSamePath(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	var gotGET, gotPOST bool
	p.MountPage(resource.PageSpec{
		Path:    "settings",
		Handler: func(w http.ResponseWriter, _ *http.Request) { gotGET = true; w.WriteHeader(http.StatusOK) },
	})
	p.MountPage(resource.PageSpec{
		Path:    "settings",
		Method:  http.MethodPost,
		Handler: func(w http.ResponseWriter, _ *http.Request) { gotPOST = true; w.WriteHeader(http.StatusOK) },
	})
	cookieVal := authCookie(t, a, "admin", "secret")

	for _, m := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequestWithContext(context.Background(), m, "/admin/settings/", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s /admin/settings/: expected 200, got %d", m, w.Code)
		}
	}
	if !gotGET || !gotPOST {
		t.Fatalf("expected both handlers to run, got GET=%v POST=%v", gotGET, gotPOST)
	}
}

// TestMountPage_Method_IndexWithMethodPanics verifies the index override
// (Path:"") rejects a non-empty Method — the index route is always GET.
func TestMountPage_Method_IndexWithMethodPanics(t *testing.T) {
	p := panelWithAuth(newTestAuth())
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic mounting Path:\"\" with a non-empty Method, got none")
		}
	}()
	p.MountPage(resource.PageSpec{
		Path:    "",
		Method:  http.MethodPost,
		Handler: func(http.ResponseWriter, *http.Request) {},
	})
}
