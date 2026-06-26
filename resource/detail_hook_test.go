package resource_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// detailHookResource builds a Resource with a Detail closure for testing.
func detailHookResource(name string, detail func(context.Context, *http.Request, string) (string, templ.Component, error)) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: strings.ToUpper(name[:1]) + name[1:],
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Perms:  resource.ReadAny,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "first"}}}}, 1, nil
		},
		Detail: detail,
	}
}

// textComponent returns a templ.Component that writes a fixed string.
func textComponent(s string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprint(w, s)
		return err
	})
}

// TestDetail_RendersDetailPage verifies: 200, content present, back-link, shell chrome, security headers.
// Falsification: remove Detail route mounting in Register → 404.
func TestDetail_RendersDetailPage(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailHookResource("widgets", func(_ context.Context, _ *http.Request, id string) (string, templ.Component, error) {
		return "Widget " + id, textComponent("<p>widget-content-" + id + "</p>"), nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/42", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// Content from hook
	if !strings.Contains(body, "widget-content-42") {
		t.Errorf("expected hook content in body")
	}
	// Back-link to list
	if !strings.Contains(body, "/admin/widgets") {
		t.Errorf("expected back-link to /admin/widgets in body")
	}
	// Shell chrome — sidebar is present (nav item for "widgets" registered by Register).
	if !strings.Contains(body, `class="sidebar"`) {
		t.Errorf("expected sidebar chrome in body")
	}
	// Nav item for the resource should appear.
	if !strings.Contains(body, "Widgets") {
		t.Errorf("expected nav item 'Widgets' in sidebar")
	}
	// Security headers
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("expected Content-Security-Policy header")
	}
	if w.Header().Get("X-Frame-Options") == "" {
		t.Errorf("expected X-Frame-Options header")
	}
}

// TestDetail_ErrDetailNotFound returns 404.
// Falsification: remove ErrDetailNotFound check in detailHookHandler → 500 or 200.
func TestDetail_ErrDetailNotFound(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailHookResource("widgets", func(_ context.Context, _ *http.Request, id string) (string, templ.Component, error) {
		return "", nil, resource.ErrDetailNotFound
	}))

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/missing", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ErrDetailNotFound, got %d", w.Code)
	}
}

// TestDetail_ArbitraryError returns 500.
// Falsification: remove error path in detailHookHandler → panic or 200.
func TestDetail_ArbitraryError(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailHookResource("widgets", func(_ context.Context, _ *http.Request, id string) (string, templ.Component, error) {
		return "", nil, errors.New("db down")
	}))

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/bad", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for arbitrary error, got %d", w.Code)
	}
}

// TestDetail_RowsNotShadowed is the CRITICAL routing test.
// /rows must always reach the list rows partial (literal beats wildcard in Go 1.22 ServeMux).
// /42 must reach the detail handler.
// Falsification: /rows going to detail handler instead of list partial = test RED.
func TestDetail_RowsNotShadowed(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})

	detailCalled := false
	resource.Register(p, detailHookResource("items", func(_ context.Context, _ *http.Request, id string) (string, templ.Component, error) {
		detailCalled = true
		return "Item " + id, textComponent("detail:" + id), nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	t.Run("/rows reaches list partial, not detail", func(t *testing.T) {
		detailCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/rows", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /rows, got %d", w.Code)
		}
		if detailCalled {
			t.Errorf("/rows must NOT reach the Detail hook; it reached detail handler instead")
		}
		// List rows fragment must not contain detail content
		if strings.Contains(w.Body.String(), "detail:rows") {
			t.Errorf("/rows response looks like detail output, not list partial")
		}
	})

	t.Run("/42 reaches detail handler", func(t *testing.T) {
		detailCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /42, got %d\nbody: %s", w.Code, w.Body.String())
		}
		if !detailCalled {
			t.Errorf("/42 must reach the Detail hook; it did not")
		}
		if !strings.Contains(w.Body.String(), "detail:42") {
			t.Errorf("expected detail:42 in response body")
		}
	})
}

// TestDetail_NoHookNoRoute verifies no Detail hook = no /{id} route, no panic.
// Falsification: if Detail=nil still mounts the route → /anything returns non-404.
func TestDetail_NoHookNoRoute(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, minimalResource("items")) // Detail == nil

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/anything", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when Detail is nil, got %d", w.Code)
	}
}

// TestDetail_WriterAndDetailCoexist verifies both Writer and Detail can be registered.
// /{id}/edit must reach the edit handler; /{id} must reach the detail handler.
// No ServeMux panic at Register time.
// Falsification: remove either route → 404 on the expected path.
func TestDetail_WriterAndDetailCoexist(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
		CSRFKey:  []byte("test-csrf-key-32-bytes-long-here"),
	})

	detailCalled := false
	res := resource.Resource{
		Name:  "things",
		Title: "Things",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Perms:  resource.ReadAny,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
		Detail: func(_ context.Context, _ *http.Request, id string) (string, templ.Component, error) {
			detailCalled = true
			return "Thing " + id, textComponent("detail:" + id), nil
		},
		Writer: &resource.Writer{
			Form: resource.FormSpec{
				Fields: []resource.Field{
					{Key: "name", Label: "Name", Kind: resource.FieldText, Required: true},
				},
			},
			Load: func(_ context.Context, _ tenant.Tenant, id string) (map[string]string, error) {
				return map[string]string{"name": "test"}, nil
			},
			Save: func(_ context.Context, _ tenant.Tenant, id string, vals map[string]string) error {
				return nil
			},
			WriteAny: true,
		},
	}

	// Must not panic.
	resource.Register(p, res)

	cookieVal := authCookie(t, a, "admin", "secret")

	t.Run("GET /{id}/edit reaches edit handler", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/42/edit", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /{id}/edit, got %d\nbody: %s", w.Code, w.Body.String())
		}
		if detailCalled {
			t.Errorf("/{id}/edit must NOT reach the Detail hook")
		}
	})

	t.Run("GET /{id} reaches detail handler", func(t *testing.T) {
		detailCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/42", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /{id}, got %d\nbody: %s", w.Code, w.Body.String())
		}
		if !detailCalled {
			t.Errorf("/{id} must reach the Detail hook")
		}
	})
}
