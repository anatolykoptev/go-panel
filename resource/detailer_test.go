package resource_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// detailerResource builds a Resource with a Detailer closure for testing.
func detailerResource(name string, detailer func(context.Context, *http.Request, string) ([]resource.DetailSection, error)) resource.Resource {
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
		Detailer: detailer,
	}
}

// TestDetailer_RouteNotMountedWhenNil asserts that a resource without a Detailer
// mounts NO /{name}/{id} route — GET /admin/items/42 returns 404 (not found).
// Falsification: if Detailer-nil no longer blocks the route, this test goes RED.
func TestDetailer_RouteNotMountedWhenNil(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, minimalResource("items")) // Detailer == nil

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	// No Detailer → no route → 404.
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for resource without Detailer, got %d\nbody: %s", w.Code, w.Body.String())
	}
}

// TestDetailer_RendersDetailPage asserts that a resource with a Detailer serves
// GET /admin/things/99 as a 200 page containing the section title and item values.
// Falsification: remove the Detailer route mounting in Register → 404.
func TestDetailer_RendersDetailPage(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("things", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{
			{
				Title: "Overview",
				Items: []resource.DetailItem{
					{Label: "ID", Value: id},
					{Label: "Status", Value: "published"},
				},
			},
		}, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/99", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Overview") {
		t.Errorf("expected section title 'Overview' in detail page body")
	}
	if !strings.Contains(body, "99") {
		t.Errorf("expected id '99' in detail page body")
	}
	if !strings.Contains(body, "published") {
		t.Errorf("expected item value 'published' in detail page body")
	}
	// Back link should point to the list.
	if !strings.Contains(body, "/admin/things") {
		t.Errorf("expected back-link to /admin/things in detail page body")
	}
}

// TestDetailer_IDNewReturns404 asserts id=="new" is rejected with 404.
// Falsification: remove the id=="new" guard in detailHandler → 200.
func TestDetailer_IDNewReturns404(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("things", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return nil, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/new", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for id=new, got %d", w.Code)
	}
}

// TestDetailer_RequiresAuth asserts the detail page is behind auth (unauthenticated → redirect).
// Falsification: remove p.auth.Require wrapping in mountDetailRoute → 200 unauthenticated.
func TestDetailer_RequiresAuth(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("things", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return nil, nil
	}))

	// No cookie — unauthenticated.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/42", nil)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	// Auth redirects to login; we expect a 3xx.
	if w.Code < 300 || w.Code >= 400 {
		t.Errorf("expected redirect for unauthenticated detail request, got %d", w.Code)
	}
}

// TestDetailer_500OnError asserts a Detailer closure error returns 500.
// Falsification: remove the error-path in detailHandler → goroutine panic or silent 200.
func TestDetailer_500OnError(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("things", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return nil, errors.New("db connection refused")
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/42", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for detailer error, got %d", w.Code)
	}
}

// TestDetailer_HTMLItemRendered asserts that a DetailItem with HTML=true renders
// the raw HTML (not escaped), while HTML=false escapes it.
// Falsification: swap templ.Raw path with escaped path → chip HTML appears escaped.
func TestDetailer_HTMLItemRendered(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	chipHTML := `<span class="fit-chip fit-strong">str</span>`
	resource.Register(p, detailerResource("things", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{
			{
				Title: "Chips",
				Items: []resource.DetailItem{
					{Label: "Fit", Value: chipHTML, HTML: true},
					{Label: "Raw", Value: "<b>bold</b>", HTML: false},
				},
			},
		}, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/1", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// HTML=true item: raw HTML must appear unescaped.
	if !strings.Contains(body, `class="fit-chip fit-strong"`) {
		t.Errorf("HTML=true item: expected chip class in output, got escaped or missing")
	}
	// HTML=false item: the < must be escaped.
	if strings.Contains(body, `<b>bold</b>`) {
		t.Errorf("HTML=false item: expected <b> to be HTML-escaped, but got raw")
	}
}

// TestDetailer_RawHTMLSectionRendered asserts that a DetailSection with RawHTML
// emits the raw block unescaped.
// Falsification: escape RawHTML in detail.templ → HTML appears as entities.
func TestDetailer_RawHTMLSectionRendered(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	panel := `<div class="fit-card"><div class="fit-card-header">FIT ASSESSMENT</div></div>`
	resource.Register(p, detailerResource("things", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{
			{Title: "", RawHTML: panel},
		}, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/1", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `class="fit-card"`) {
		preview := body
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Errorf("expected RawHTML fit-card class in output; got: %q", preview)
	}
}

// TestDetailer_RowsNotShadowed is the CRITICAL routing test.
// /rows must always reach the list rows partial (literal beats wildcard in Go 1.22 ServeMux).
// /42 must reach the detail handler.
// Falsification: /rows going to detail handler instead of list partial = test RED.
func TestDetailer_RowsNotShadowed(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})

	detailerCalled := false
	resource.Register(p, detailerResource("items", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		detailerCalled = true
		return []resource.DetailSection{{Title: "detail-section-" + id}}, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")

	t.Run("/rows reaches list partial, not detailer", func(t *testing.T) {
		detailerCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/rows", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /rows, got %d", w.Code)
		}
		if detailerCalled {
			t.Errorf("/rows must NOT reach the Detailer; it reached detailer instead")
		}
		// List rows fragment must not contain detail section content
		if strings.Contains(w.Body.String(), "detail-section-rows") {
			t.Errorf("/rows response looks like detail output, not list partial")
		}
	})

	t.Run("/42 reaches detailer", func(t *testing.T) {
		detailerCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /42, got %d\nbody: %s", w.Code, w.Body.String())
		}
		if !detailerCalled {
			t.Errorf("/42 must reach the Detailer; it did not")
		}
		if !strings.Contains(w.Body.String(), "detail-section-42") {
			t.Errorf("expected detail-section-42 in response body")
		}
	})
}

// TestDetailer_ErrDetailNotFound returns 404.
// Falsification: remove ErrDetailNotFound check in detailHandler → 500.
func TestDetailer_ErrDetailNotFound(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("widgets", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return nil, resource.ErrDetailNotFound
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

// TestDetailer_ArbitraryErrorReturns500 asserts a non-sentinel error returns 500.
// Falsification: remove non-sentinel error path in detailHandler → panic or 200.
func TestDetailer_ArbitraryErrorReturns500(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("widgets", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return nil, errors.New("boom")
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

// TestDetailer_WriterAndDetailerCoexist verifies both Writer and Detailer can be registered.
// /{id}/edit must reach the edit handler; /{id} must reach the detailer.
// No ServeMux panic at Register time.
// Falsification: remove either route → 404 on the expected path.
func TestDetailer_WriterAndDetailerCoexist(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
		CSRFKey:  []byte("test-csrf-key-32-bytes-long-here"),
	})

	detailerCalled := false
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
		Detailer: func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
			detailerCalled = true
			return []resource.DetailSection{{Title: "detail-" + id}}, nil
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
		detailerCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/42/edit", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /{id}/edit, got %d\nbody: %s", w.Code, w.Body.String())
		}
		if detailerCalled {
			t.Errorf("/{id}/edit must NOT reach the Detailer")
		}
	})

	t.Run("GET /{id} reaches detailer", func(t *testing.T) {
		detailerCalled = false
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/things/42", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /{id}, got %d\nbody: %s", w.Code, w.Body.String())
		}
		if !detailerCalled {
			t.Errorf("/{id} must reach the Detailer; it did not")
		}
	})
}

// TestDetailer_SecurityHeadersAndNav verifies the detail page includes security headers,
// active nav state, and a back-link.
// Falsification: remove shell.SecurityHeaders call → headers absent.
func TestDetailer_SecurityHeadersAndNav(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("widgets", func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "Widget Detail", Items: []resource.DetailItem{{Label: "ID", Value: id}}}}, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/42", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}

	// Security headers
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("expected Content-Security-Policy header")
	}
	if w.Header().Get("X-Frame-Options") == "" {
		t.Errorf("expected X-Frame-Options header")
	}

	body := w.Body.String()
	// Nav item for the resource should appear (active nav).
	if !strings.Contains(body, "Widgets") {
		t.Errorf("expected nav item 'Widgets' in sidebar")
	}
	// Back-link to list.
	if !strings.Contains(body, "/admin/widgets") {
		t.Errorf("expected back-link to /admin/widgets")
	}
}

// TestDetailer_RequestHeaderWiredThrough proves *http.Request is wired to the closure.
// The Detailer reads a custom header from r and reflects it in the section title.
// Falsification: revert Detailer to (ctx, id) signature → header read fails or returns "".
func TestDetailer_RequestHeaderWiredThrough(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, detailerResource("widgets", func(_ context.Context, r *http.Request, id string) ([]resource.DetailSection, error) {
		headerVal := r.Header.Get("X-Test")
		if headerVal == "" {
			headerVal = "missing"
		}
		return []resource.DetailSection{{Title: "hdr-" + headerVal}}, nil
	}))

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/1", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	req.Header.Set("X-Test", "wired")
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hdr-wired") {
		t.Errorf("expected 'hdr-wired' in response body — *http.Request not wired through to Detailer")
	}
}

// TestColWidthAlignInListHeader asserts that Column.Width and Column.Align
// are emitted as inline styles on <th> elements.
// Falsification: remove colHeaderStyle call from list.templ → styles absent.
func TestColWidthAlignInListHeader(t *testing.T) {
	a := newTestAuth()
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	res := resource.Resource{
		Name:  "widgets",
		Title: "Widgets",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Label: "Name", Sortable: true, SQLExpr: "w.name", Width: "12rem"},
				{Key: "score", Label: "Score", Sortable: false, SQLExpr: "w.score", Width: "5rem", Align: "right"},
			},
			DefaultKey: "name",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Perms:  resource.ReadAny,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) { return nil, 0, nil },
	}
	resource.Register(p, res)

	cookieVal := authCookie(t, a, "admin", "secret")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "width:12rem") {
		t.Errorf("expected width:12rem style on <th> for 'name' column, not found in body")
	}
	if !strings.Contains(body, "width:5rem") {
		t.Errorf("expected width:5rem style on <th> for 'score' column, not found in body")
	}
	if !strings.Contains(body, "text-align:right") {
		t.Errorf("expected text-align:right style on <th> for 'score' column, not found in body")
	}
}
