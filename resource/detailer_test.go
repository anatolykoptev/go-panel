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
)

// detailerResource builds a Resource with a Detailer closure for testing.
func detailerResource(name string, detailer func(context.Context, string) ([]resource.DetailSection, error)) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: strings.ToUpper(name[:1]) + name[1:],
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter:   admintable.FilterSpec{},
		Perms:    resource.ReadAny,
		Lister:   func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) { return nil, 0, nil },
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
	resource.Register(p, detailerResource("things", func(_ context.Context, id string) ([]resource.DetailSection, error) {
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
	resource.Register(p, detailerResource("things", func(_ context.Context, id string) ([]resource.DetailSection, error) {
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
	resource.Register(p, detailerResource("things", func(_ context.Context, id string) ([]resource.DetailSection, error) {
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
	resource.Register(p, detailerResource("things", func(_ context.Context, id string) ([]resource.DetailSection, error) {
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
	resource.Register(p, detailerResource("things", func(_ context.Context, id string) ([]resource.DetailSection, error) {
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
	resource.Register(p, detailerResource("things", func(_ context.Context, id string) ([]resource.DetailSection, error) {
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

