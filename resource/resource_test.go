package resource_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
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

// TestListPage_ListerErrorDoesNotLeakDetails verifies that a Lister failure
// (e.g. a raw pgx/SQL error) produces a generic 500 body — the underlying
// error text must never reach the HTTP response.
func TestListPage_ListerErrorDoesNotLeakDetails(t *testing.T) {
	p := newTestPanel()
	leaky := testResource
	sensitive := "pq: relation \"items\" does not exist (connection to db-primary.internal:5432)"
	leaky.Lister = func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
		return nil, 0, errors.New(sensitive)
	}
	resource.Register(p, leaky)

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
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), sensitive) {
		t.Errorf("response body leaked the raw Lister error: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "db-primary.internal") {
		t.Errorf("response body leaked infrastructure details: %s", w.Body.String())
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

// TestPaginationPreservesFilterParams verifies that the prev/next pagination
// links rendered on a filtered list page keep all filter query params.
//
// Regression guard: before the fix, pageURL() built the link from scratch,
// dropping every param except page + sort/dir. Clicking Next on a filtered
// list silently reset the filter. The test goes red when that behavior is
// present.
func TestPaginationPreservesFilterParams(t *testing.T) {
	// Resource with a status filter that the handler will parse and the
	// rendered HTML's page links must carry forward.
	p := newTestPanel()
	res := resource.Resource{
		Name:  "orders",
		Title: "Orders",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "name", Sortable: true, SQLExpr: "o.name"}},
			DefaultKey: "name",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "status", SQLExpr: "o.status", Match: admintable.Eq, Allowed: []string{"active", "inactive"}},
		}},
		Scope: tenant.Scope{},
		// Lister returns 2 pages worth of rows (total=100, default pageSize=50)
		// so that the pagination widget renders a Next link.
		Lister: func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "X"}}}}, 100, nil
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

	// Request page 1 with a status filter applied.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/admin/orders/rows?status=active&page=1", nil)
	req.Header.Set("HX-Request", "true") // get the fragment (rows + pagination)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}

	html := w.Body.String()

	// The "Next" pagination link must carry status=active forward.
	// Before the fix: the link would be /rows?page=2 (filter dropped).
	// After the fix:  the link contains status=active AND page=2.
	if !strings.Contains(html, "status=active") {
		t.Errorf("pagination link dropped the filter param: status=active not found in rendered HTML.\n"+
			"This is the regression: pageURL() did not preserve filter query params.\n"+
			"Rendered HTML (truncated):\n%s", html[:min(len(html), 2000)])
	}
	if !strings.Contains(html, "page=2") {
		t.Errorf("expected page=2 in pagination link, not found.\nRendered HTML:\n%s",
			html[:min(len(html), 2000)])
	}
}

// TestResourceBadgeWiredToNavItem verifies that Register wires Resource.Badge
// onto the corresponding NavItem.Badge so the template can call it at render time.
// Falsification: remove `Badge: r.Badge` from the nav append in Register →
// badgeItem.Badge is nil and the test fails.
func TestResourceBadgeWiredToNavItem(t *testing.T) {
	p := newTestPanel()
	var callCount int
	res := resource.Resource{
		Name:  "badgetest",
		Title: "Badge Test",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
		Badge: func(_ context.Context) string {
			callCount++
			return "7"
		},
	}
	resource.Register(p, res)

	nav := p.NavItems()
	var found *shell.NavItem
	for i := range nav {
		if nav[i].ID == "badgetest" {
			found = &nav[i]
			break
		}
	}
	if found == nil {
		t.Fatal("nav item for badgetest not registered")
	}
	if found.Badge == nil {
		t.Fatal("Resource.Badge was not wired to NavItem.Badge by Register")
	}
	val := found.Badge(context.Background())
	if val != "7" {
		t.Fatalf("Badge closure returned %q, want \"7\"", val)
	}
	if callCount != 1 {
		t.Fatalf("expected Badge called once, got %d", callCount)
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

// roleCapableAuth is a stub authenticator that implements resource.RoleAuthenticator.
// RequireRole records each role it is asked to gate (and forwards to next) so a test
// can assert that p.guard routes a role-gated resource's routes through RequireRole.
type roleCapableAuth struct{ gated []string }

func (a *roleCapableAuth) Require(h http.HandlerFunc) http.HandlerFunc { return h }
func (a *roleCapableAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}
func (a *roleCapableAuth) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}
func (a *roleCapableAuth) RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	a.gated = append(a.gated, role)
	return next
}
func (a *roleCapableAuth) HasRole(context.Context, string) bool { return true }

// TestGuard_FailsClosed_WhenAuthLacksCapability verifies that Register panics when a
// resource declares a non-empty RequiredRole but the configured authenticator does not
// implement resource.RoleAuthenticator — the fail-closed role-gating guarantee. Mirrors
// the Writer/CSRF Register-time panic tests.
func TestGuard_FailsClosed_WhenAuthLacksCapability(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when RequiredRole is set but auth lacks RoleAuthenticator capability")
		}
	}()
	// noSessionNameAuth (writer_test.go) implements only Require/LoginHandler/
	// LogoutHandler — NOT RoleAuthenticator — so a role-gated resource must not mount.
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     noSessionNameAuth{},
	})
	r := testResource
	r.RequiredRole = "admin"
	resource.Register(p, r)
}

// TestGuard_RoleGatedResource_RoutesThroughRequireRole verifies that, when the
// authenticator implements RoleAuthenticator, p.guard routes a role-gated resource's
// routes through RequireRole(role) (the enforcement authority) rather than the bare
// Require. Goes RED if the route mounts bypass guard or call Require directly.
func TestGuard_RoleGatedResource_RoutesThroughRequireRole(t *testing.T) {
	ra := &roleCapableAuth{}
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     ra,
	})
	r := testResource
	r.RequiredRole = "admin"
	resource.Register(p, r) // must NOT panic: auth implements RoleAuthenticator

	gated := 0
	for _, role := range ra.gated {
		if role == "admin" {
			gated++
		}
	}
	// testResource mounts list + rows (no Detailer, no Writer) -> 2 RequireRole calls.
	if gated < 2 {
		t.Fatalf("expected the list+rows routes gated via RequireRole(%q), got %d gated calls: %v", "admin", gated, ra.gated)
	}
}
