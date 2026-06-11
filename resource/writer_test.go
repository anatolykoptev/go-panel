package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// testCSRFKey is a 32-byte key for CSRF token signing in tests.
var testCSRFKey = []byte("test-csrf-key-32-bytes-long-here")

// newWriterPanel creates a Panel with CSRFKey set.
func newWriterPanel() *resource.Panel {
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
		CSRFKey:  testCSRFKey,
	})
}

// loginAndGetCookie performs a login and returns the session cookie value.
func loginAndGetCookie(t *testing.T, p *resource.Panel) (cookieVal, cookieHeader string) {
	t.Helper()
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
	hdr := loginW.Header().Get("Set-Cookie")
	return extractCookieValue(hdr, "panel_admin"), hdr
}

// writerResource builds a test resource with a Writer.
func writerResource(loadFn func(context.Context, tenant.Tenant, string) (map[string]string, error),
	saveFn func(context.Context, tenant.Tenant, string, map[string]string) error,
) resource.Resource {
	r := testResource // copy
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{Key: "name", Label: "Name", Kind: resource.FieldText, Required: true},
				{Key: "status", Label: "Status", Kind: resource.FieldSelect, Options: []resource.Option{
					{Value: "active", Label: "Active"},
					{Value: "inactive", Label: "Inactive"},
				}},
				{Key: "note", Label: "Note", Kind: resource.FieldJSON},
			},
		},
		Load:     loadFn,
		Save:     saveFn,
		WriteAny: true,
	}
	return r
}

// TestWriterRoutes_NewFormRendersFields verifies GET /new renders form fields.
func TestWriterRoutes_NewFormRendersFields(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/new", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="name"`) {
		t.Errorf("expected name field in form, body snippet: %s", body[:min(len(body), 500)])
	}
	if !strings.Contains(body, `name="status"`) {
		t.Errorf("expected status field in form")
	}
	if !strings.Contains(body, `name="_csrf"`) {
		t.Errorf("expected _csrf hidden field in form")
	}
}

// TestWriterRoutes_EditFormCallsLoad verifies GET /{id}/edit calls Load with the correct id.
func TestWriterRoutes_EditFormCallsLoad(t *testing.T) {
	loadedID := ""
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, id string) (map[string]string, error) {
			loadedID = id
			return map[string]string{"name": "Test Item", "status": "active", "note": "{}"}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42/edit", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if loadedID != "42" {
		t.Errorf("Load called with id %q, expected %q", loadedID, "42")
	}
	body := w.Body.String()
	if !strings.Contains(body, "Test Item") {
		t.Errorf("expected loaded value 'Test Item' in form, body: %s", body[:min(len(body), 500)])
	}
}

// TestWriterRoutes_PostWithoutCSRF verifies POST without CSRF token returns 403.
func TestWriterRoutes_PostWithoutCSRF(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			t.Error("Save should not be called when CSRF missing")
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	form := url.Values{"name": {"Test"}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// TestWriterRoutes_PostWithBadCSRF verifies POST with invalid CSRF token returns 403.
func TestWriterRoutes_PostWithBadCSRF(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			t.Error("Save should not be called when CSRF invalid")
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	form := url.Values{
		"name":  {"Test"},
		"_csrf": {"tampered.invalidtoken"},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// TestWriterRoutes_PostValid verifies a valid POST calls Save and redirects.
func TestWriterRoutes_PostValid(t *testing.T) {
	var savedID string
	var savedValues map[string]string
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, id string, values map[string]string) error {
			savedID = id
			savedValues = values
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	// Issue a valid CSRF token for the session cookie value.
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":   {"My Item"},
		"status": {"active"},
		"note":   {"{}"},
		"_csrf":  {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if savedValues == nil {
		t.Fatal("Save was not called")
	}
	if savedValues["name"] != "My Item" {
		t.Errorf("expected name='My Item', got %q", savedValues["name"])
	}
	if savedID != "" {
		t.Errorf("expected empty id for create, got %q", savedID)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/items" {
		t.Errorf("expected redirect to /admin/items, got %q", loc)
	}
}

// TestWriterRoutes_PostRequiredMissing verifies missing required field returns 422, Save not called.
func TestWriterRoutes_PostRequiredMissing(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	// name is required but omitted.
	form := url.Values{
		"_csrf": {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save should not be called on validation error, called %d times", saveCount)
	}
	if !strings.Contains(w.Body.String(), "required") {
		t.Errorf("expected 'required' in error response body")
	}
}

// TestWriterRoutes_PostSelectOutOfWhitelist verifies out-of-whitelist select value returns 422.
func TestWriterRoutes_PostSelectOutOfWhitelist(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":   {"My Item"},
		"status": {"deleted"}, // not in whitelist
		"_csrf":  {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save should not be called on validation error")
	}
}

// TestWriterRoutes_PostInvalidJSON verifies invalid JSON in FieldJSON returns 422.
func TestWriterRoutes_PostInvalidJSON(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":  {"My Item"},
		"note":  {"{invalid json"},
		"_csrf": {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for invalid JSON, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save should not be called on validation error")
	}
}

// TestWriterRoutes_ReadOnlyResource verifies /new returns 404 when Writer is nil.
func TestWriterRoutes_ReadOnlyResource(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, testResource) // testResource has no Writer
	cookieVal, _ := loginAndGetCookie(t, p)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/new", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	// Route not registered for read-only resource → 404.
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for read-only resource /new, got %d", w.Code)
	}
}

// TestWriterRoutes_RegisterPanicsWithoutCSRFKey verifies panic when Writer set but no CSRFKey.
func TestWriterRoutes_RegisterPanicsWithoutCSRFKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Writer set but CSRFKey empty")
		}
	}()
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	// No CSRFKey — should panic.
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
	})
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return nil, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	))
}

// TestFormSpec_Valid verifies FormSpec validation rules.
func TestFormSpec_Valid(t *testing.T) {
	cases := []struct {
		name    string
		spec    resource.FormSpec
		wantErr bool
	}{
		{
			name: "valid",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "name", Label: "Name", Kind: resource.FieldText},
			}},
			wantErr: false,
		},
		{
			name: "empty key",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "", Label: "Name", Kind: resource.FieldText},
			}},
			wantErr: true,
		},
		{
			name: "empty label",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "name", Label: "", Kind: resource.FieldText},
			}},
			wantErr: true,
		},
		{
			name: "duplicate key",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "name", Label: "Name", Kind: resource.FieldText},
				{Key: "name", Label: "Name2", Kind: resource.FieldText},
			}},
			wantErr: true,
		},
		{
			name: "select without options",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "status", Label: "Status", Kind: resource.FieldSelect},
			}},
			wantErr: true,
		},
		{
			name: "select with options OK",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "status", Label: "Status", Kind: resource.FieldSelect,
					Options: []resource.Option{{Value: "a", Label: "A"}}},
			}},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Valid()
			if (err != nil) != tc.wantErr {
				t.Errorf("Valid() = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
