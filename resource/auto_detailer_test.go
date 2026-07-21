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

// Auto-Detailer tests — when Detailer is nil but FetchRow is non-nil, Register
// synthesizes a Detailer from Sort.Columns + FetchRow. This solves the
// cross-resource linking 404 problem: resources without a hand-written
// Detailer can still serve a basic detail page so cross-link cells work.

// fetchRowResource builds a Resource with FetchRow but NO Detailer.
func fetchRowResource(name string, fetchRow func(context.Context, string) (map[string]string, error)) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: strings.ToUpper(name[:1]) + name[1:],
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", Label: "ID", Sortable: true, SQLExpr: "t.id"},
				{Key: "name", Label: "Name", Sortable: false, SQLExpr: "t.name"},
				{Key: "status", Label: "Status", Sortable: false, SQLExpr: "t.status"},
			},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "first"}}}}, 1, nil
		},
		FetchRow: fetchRow,
	}
}

// TestAutoDetailer_RouteMountedWithFetchRow asserts that a resource with
// FetchRow (but no Detailer) DOES mount /{name}/{id} and serves a 200 page.
// Falsification: if Register ignores FetchRow, this goes RED (404).
func TestAutoDetailer_RouteMountedWithFetchRow(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, fetchRowResource("widgets", func(ctx context.Context, id string) (map[string]string, error) {
		return map[string]string{
			"id":     id,
			"name":   "Widget " + id,
			"status": "active",
		}, nil
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/42", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for resource with FetchRow, got %d\nbody: %s", w.Code, w.Body.String())
	}
}

// TestAutoDetailer_RendersColumnLabelsAndValues asserts the auto-generated
// detail page shows each Sort.Columns entry as a DetailItem with the column
// Label and the FetchRow-returned value.
// Falsification: if auto-Detailer drops columns or swaps Label/Value, this goes RED.
func TestAutoDetailer_RendersColumnLabelsAndValues(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, fetchRowResource("widgets", func(ctx context.Context, id string) (map[string]string, error) {
		return map[string]string{
			"id":     id,
			"name":   "Gadget",
			"status": "published",
		}, nil
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/7", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// Each column Label should appear (from Sort.Columns).
	for _, label := range []string{"ID", "Name", "Status"} {
		if !strings.Contains(body, label) {
			t.Errorf("expected column label %q in body, missing", label)
		}
	}
	// Each FetchRow value should appear.
	for _, val := range []string{"7", "Gadget", "published"} {
		if !strings.Contains(body, val) {
			t.Errorf("expected value %q in body, missing", val)
		}
	}
}

// TestAutoDetailer_ErrDetailNotFound asserts that FetchRow returning
// ErrDetailNotFound results in a 404 (same sentinel as hand-written Detailer).
func TestAutoDetailer_ErrDetailNotFound(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, fetchRowResource("widgets", func(ctx context.Context, id string) (map[string]string, error) {
		return nil, resource.ErrDetailNotFound
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/999", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ErrDetailNotFound, got %d", w.Code)
	}
}

// TestAutoDetailer_500OnError asserts that FetchRow returning a non-sentinel
// error results in a 500.
func TestAutoDetailer_500OnError(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, fetchRowResource("widgets", func(ctx context.Context, id string) (map[string]string, error) {
		return nil, errors.New("db down")
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/1", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for arbitrary error, got %d", w.Code)
	}
}

// TestAutoDetailer_IDNewReturns404 asserts id=="new" is rejected even with
// FetchRow (symmetric with hand-written Detailer).
func TestAutoDetailer_IDNewReturns404(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, fetchRowResource("widgets", func(ctx context.Context, id string) (map[string]string, error) {
		t.Errorf("FetchRow should not be called for id==new")
		return nil, nil
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/new", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for id==new, got %d", w.Code)
	}
}

// TestAutoDetailer_DetailerOverridesFetchRow asserts that when BOTH Detailer
// and FetchRow are set, the hand-written Detailer wins (FetchRow ignored).
func TestAutoDetailer_DetailerOverridesFetchRow(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	r := fetchRowResource("widgets", func(ctx context.Context, id string) (map[string]string, error) {
		t.Errorf("FetchRow should not be called when Detailer is set")
		return nil, nil
	})
	r.Detailer = func(ctx context.Context, req *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "Custom", Items: []resource.DetailItem{{Label: "L", Value: "V"}}}}, nil
	}
	resource.Register(p, r)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/widgets/1", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Custom") {
		t.Errorf("expected hand-written Detailer output, got: %s", w.Body.String())
	}
}

// TestAutoDetailer_BothNilStill404 asserts that when BOTH Detailer and FetchRow
// are nil, the detail route is NOT mounted (existing behaviour preserved).
// This is the backward-compatibility guard.
func TestAutoDetailer_BothNilStill404(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, minimalResource("items")) // Detailer == nil, FetchRow == nil

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42", nil)
	cookieVal := authCookie(t, a, "admin", "secret")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when both Detailer and FetchRow are nil, got %d", w.Code)
	}
}
