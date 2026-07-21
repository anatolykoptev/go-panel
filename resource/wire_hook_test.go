package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// TestMakeListHandler_ResolveRelationsWired proves the resolveRelations hook
// is actually wired into makeListHandler (Phase 3a): a registered Resource
// declaring a BelongsTo Relation with a ResolveLabels closure must, when its
// list endpoint is hit over real HTTP, render the XSS-safe CrossLinkCell
// anchor in place of the raw foreign key. This exercises the REAL shipped
// code path (Panel.Handler -> mux -> makeListHandler -> resolveRelations ->
// list template templ.Raw(cell.Value)) — not resolveRelations in isolation.
//
// This test goes RED if the resolveRelations call is removed from
// makeListHandler: without the hook the author_id cell renders the raw FK
// ("42") as escaped text, so neither the anchor HTML nor the resolved label
// ("Alice") appears in the response body.
func TestMakeListHandler_ResolveRelationsWired(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)

	// Target "users" resource — registered for realism (nav + detail route).
	// The ResolveLabels path does not consult it, but registering mirrors
	// real consumer usage and keeps the panel well-formed.
	resource.Register(p, resource.Resource{
		Name:  "users",
		Title: "Users",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Label: "ID", Sortable: true, SQLExpr: "u.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
	})

	// Source "posts" resource declaring a BelongsTo Relation on author_id.
	// The Lister returns a single row whose author_id cell holds the raw FK
	// "42"; resolveRelations must replace it with a CrossLinkCell anchor.
	postsRes := resource.Resource{
		Name:  "posts",
		Title: "Posts",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", Label: "ID", Sortable: true, SQLExpr: "p.id"},
				{Key: "author_id", Label: "Author", Sortable: true, SQLExpr: "p.author_id"},
			},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{
				{ID: "1", Cells: []resource.Cell{{Value: "1"}, {Value: "42"}}},
			}, 1, nil
		},
		Relations: []resource.Relation{
			{
				Resource:   "users",
				ForeignKey: "author_id",
				DisplayKey: "name",
				ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
					return map[string]string{"42": "Alice"}, nil
				},
			},
		},
	}
	resource.Register(p, postsRes)

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/posts", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// The CrossLinkCell anchor must be present (raw HTML via templ.Raw).
	wantAnchor := `<a href="/admin/users/42">Alice</a>`
	if !strings.Contains(body, wantAnchor) {
		t.Errorf("response missing cross-link anchor %q — resolveRelations hook not wired?\nbody snippet:\n%s", wantAnchor, body)
	}
	// The resolved label must appear; the raw FK must NOT appear as a bare
	// escaped text node in the author cell. "42" may appear in the id column,
	// so we assert the anchor specifically (done above) and that "Alice" is
	// present.
	if !strings.Contains(body, "Alice") {
		t.Errorf("response missing resolved label %q — resolveRelations hook not wired?", "Alice")
	}
}

// TestMakeListHandler_NoRelations_BackwardCompat proves the
// len(r.Relations) > 0 guard is a zero-cost path: a Resource with NO
// Relations renders its raw cell value unchanged (no anchor, no mutation),
// so existing consumers keep working (ADR-5 backward compatibility).
func TestMakeListHandler_NoRelations_BackwardCompat(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)

	plainRes := resource.Resource{
		Name:  "notes",
		Title: "Notes",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", Label: "ID", Sortable: true, SQLExpr: "n.id"},
				{Key: "body", Label: "Body", Sortable: false, SQLExpr: "n.body"},
			},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{
				{ID: "7", Cells: []resource.Cell{{Value: "7"}, {Value: "hello-world"}}},
			}, 1, nil
		},
		// No Relations — guard skips resolveRelations entirely.
	}
	resource.Register(p, plainRes)

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/notes", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "hello-world") {
		t.Errorf("raw cell value missing from response (backward-compat broken): %q", "hello-world")
	}
	// No cross-link anchor should be synthesized for a relation-less resource.
	if strings.Contains(body, `<a href="/admin/`) {
		t.Errorf("unexpected cross-link anchor for relation-less resource (backward-compat broken)")
	}
}

// TestMakeListHandler_ResolveRelationsWired_FetchRowFallback proves the hook
// also covers the FetchRow fallback path (ADR-3): a Relation with nil
// ResolveLabels against a scoped target resolves via target.FetchRow and
// renders the anchor over real HTTP.
func TestMakeListHandler_ResolveRelationsWired_FetchRowFallback(t *testing.T) {
	a := newTestAuth()
	p := panelWithAuth(a)

	// Scoped target "users" with a FetchRow closure (ADR-3 tenant gate).
	resource.Register(p, resource.Resource{
		Name:  "users",
		Title: "Users",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Label: "ID", Sortable: true, SQLExpr: "u.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Scope:  tenant.Scope{Column: "u.city_slug"},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
		FetchRow: func(_ context.Context, id string) (map[string]string, error) {
			return map[string]string{"id": id, "name": "User-" + id}, nil
		},
	})

	postsRes := resource.Resource{
		Name:  "posts",
		Title: "Posts",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", Label: "ID", Sortable: true, SQLExpr: "p.id"},
				{Key: "author_id", Label: "Author", Sortable: true, SQLExpr: "p.author_id"},
			},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{
				{ID: "1", Cells: []resource.Cell{{Value: "1"}, {Value: "42"}}},
			}, 1, nil
		},
		Relations: []resource.Relation{
			{
				Resource:   "users",
				ForeignKey: "author_id",
				DisplayKey: "name",
				// nil ResolveLabels -> FetchRow fallback path.
			},
		},
	}
	resource.Register(p, postsRes)

	cookieVal := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/posts", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	wantAnchor := `<a href="/admin/users/42">User-42</a>`
	if !strings.Contains(body, wantAnchor) {
		t.Errorf("response missing cross-link anchor %q — FetchRow fallback hook not wired?\nbody snippet:\n%s", wantAnchor, body)
	}
}
