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
	"github.com/anatolykoptev/go-panel/tenant"
)

func newTestPanel() *resource.Panel {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	return resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
}

var testResource = resource.Resource{
	Name:  "items",
	Title: "Items",
	Icon:  "📦",
	Group: "Content",
	Sort: admintable.Spec{
		Columns: []admintable.Column{
			{Key: "name", Label: "Name", Sortable: true, SQLExpr: "i.name"},
			{Key: "created", Label: "Created", Sortable: true, SQLExpr: "i.created_at"},
		},
		DefaultKey: "name",
		DefaultDir: admintable.Asc,
	},
	Filter: admintable.FilterSpec{Filters: []admintable.Filter{
		{Key: "status", SQLExpr: "i.status", Match: admintable.Eq, Allowed: []string{"active", "inactive"}},
	}},
	Scope: tenant.Scope{Column: "i.city_slug"},
	Perms: resource.ReadAny,
	Lister: func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		return []resource.Row{
			{ID: "1", Cells: []resource.Cell{{Value: "Alpha"}, {Value: "2024-01-01"}}, Href: "/admin/items/1"},
			{ID: "2", Cells: []resource.Cell{{Value: "Beta"}, {Value: "2024-01-02"}}},
		}, 2, nil
	},
}

func TestRegister_PanicsOnInvalidSort(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid sort spec")
		}
	}()
	p := newTestPanel()
	resource.Register(p, resource.Resource{
		Name:  "bad",
		Title: "Bad",
		Sort:  admintable.Spec{}, // no columns — invalid
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
	})
}

func TestRegister_AddsNavItem(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, testResource)
	nav := p.NavItems()
	found := false
	for _, n := range nav {
		if n.ID == "items" {
			found = true
			if n.URL != "/admin/items" {
				t.Errorf("expected URL /admin/items, got %q", n.URL)
			}
		}
	}
	if !found {
		t.Fatal("items nav item not found")
	}
}

func TestListPage_RendersWithRows(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, testResource)

	// Inject a valid session cookie.
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	// Get a valid cookie by logging in.
	body := strings.NewReader("username=admin&password=secret")
	loginReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(loginW, loginReq)
	cookieHeader := loginW.Header().Get("Set-Cookie")
	cookieVal := extractCookieValue(cookieHeader, "panel_admin")

	// Now request the list page.
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body2 := w.Body.String()
	if !strings.Contains(body2, "Alpha") {
		t.Errorf("expected row 'Alpha' in response")
	}
	if !strings.Contains(body2, "Beta") {
		t.Errorf("expected row 'Beta' in response")
	}
	if !strings.Contains(body2, "Items") {
		t.Errorf("expected title 'Items' in response")
	}
}

func TestListPage_HTMXReturnsFragment(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, testResource)

	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	body := strings.NewReader("username=admin&password=secret")
	loginReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(loginW, loginReq)
	cookieVal := extractCookieValue(loginW.Header().Get("Set-Cookie"), "panel_admin")

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items", nil)
	r.Header.Set("HX-Request", "true")
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// htmx response should NOT contain the full html/head tags
	body2 := w.Body.String()
	if strings.Contains(body2, "<!DOCTYPE html>") {
		t.Errorf("htmx response should not contain DOCTYPE")
	}
}

func TestListPage_TenantScopeApplied(t *testing.T) {
	// Verify that the Lister receives the tenant WHERE clause for scoped resources.
	gotConds := ""
	p := newTestPanel()
	res := resource.Resource{
		Name:  "scopetest",
		Title: "Scope Test",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "name", Sortable: true, SQLExpr: "t.name"}},
			DefaultKey: "name",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Scope:  tenant.Scope{Column: "t.city_slug"},
		Perms:  resource.ReadAny,
		Lister: func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
			gotConds = q.WhereConds
			return nil, 0, nil
		},
	}
	resource.Register(p, res)

	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	body := strings.NewReader("username=admin&password=secret")
	loginReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(loginW, loginReq)
	cookieVal := extractCookieValue(loginW.Header().Get("Set-Cookie"), "panel_admin")

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/scopetest", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)

	if !strings.Contains(gotConds, "t.city_slug") {
		t.Errorf("expected tenant scope in WhereConds, got %q", gotConds)
	}
}

func extractCookieValue(setCookie, name string) string {
	prefix := name + "="
	idx := strings.Index(setCookie, prefix)
	if idx < 0 {
		return ""
	}
	rest := setCookie[idx+len(prefix):]
	end := strings.IndexAny(rest, ";,")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
