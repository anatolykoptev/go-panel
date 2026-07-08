package resource_test

import (
	"context"
	"errors"
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
		Load: loadFn,
		Save: saveFn,
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
			name: "select without options or func",
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
		{
			name: "select with OptionsFunc OK",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "status", Label: "Status", Kind: resource.FieldSelect,
					OptionsFunc: func(_ context.Context, _ tenant.Tenant) ([]resource.Option, error) {
						return []resource.Option{{Value: "a", Label: "A"}}, nil
					}},
			}},
			wantErr: false,
		},
		{
			name: "select with both Options and OptionsFunc",
			spec: resource.FormSpec{Fields: []resource.Field{
				{Key: "status", Label: "Status", Kind: resource.FieldSelect,
					Options:     []resource.Option{{Value: "a", Label: "A"}},
					OptionsFunc: func(_ context.Context, _ tenant.Tenant) ([]resource.Option, error) { return nil, nil },
				},
			}},
			wantErr: true,
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

// writerResourceWithDate builds a test resource with a FieldDate field.
func writerResourceWithDate(
	saveFn func(context.Context, tenant.Tenant, string, map[string]string) error,
) resource.Resource {
	r := testResource
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{Key: "name", Label: "Name", Kind: resource.FieldText, Required: true},
				{Key: "due", Label: "Due Date", Kind: resource.FieldDate},
			},
		},
		Load: func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		Save: saveFn,
	}
	return r
}

// TestWriterRoutes_FieldDateInvalid verifies that an invalid date returns 422 and Save is not called.
func TestWriterRoutes_FieldDateInvalid(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResourceWithDate(
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":  {"Test"},
		"due":   {"not-a-date"},
		"_csrf": {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for invalid date, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called on invalid date, called %d times", saveCount)
	}
}

// TestWriterRoutes_FieldDateValid verifies that a valid YYYY-MM-DD date passes validation.
func TestWriterRoutes_FieldDateValid(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResourceWithDate(
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":  {"Test"},
		"due":   {"2025-12-31"},
		"_csrf": {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for valid date, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Errorf("Save must be called exactly once for valid date, called %d times", saveCount)
	}
}

// writerResourceWithDateTime builds a test resource with a FieldDateTime field.
func writerResourceWithDateTime(
	saveFn func(context.Context, tenant.Tenant, string, map[string]string) error,
) resource.Resource {
	r := testResource
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{Key: "name", Label: "Name", Kind: resource.FieldText, Required: true},
				{Key: "starts", Label: "Starts At", Kind: resource.FieldDateTime},
			},
		},
		Load: func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		Save: saveFn,
	}
	return r
}

// postWriterForm posts form values against a registered writer resource and
// returns the recorded response.
func postWriterForm(t *testing.T, p *resource.Panel, cookieVal string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("_csrf", csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// TestWriterRoutes_FieldDateTimeInvalid verifies that a malformed datetime-local
// value returns 422 and Save is not called.
func TestWriterRoutes_FieldDateTimeInvalid(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResourceWithDateTime(
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := postWriterForm(t, p, cookieVal, url.Values{"name": {"Test"}, "starts": {"not-a-datetime"}})

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for invalid datetime, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called on invalid datetime, called %d times", saveCount)
	}
}

// TestWriterRoutes_FieldDateTimeValid_TFormat verifies the browser
// datetime-local submission format ("2006-01-02T15:04") passes validation.
func TestWriterRoutes_FieldDateTimeValid_TFormat(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResourceWithDateTime(
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := postWriterForm(t, p, cookieVal, url.Values{"name": {"Test"}, "starts": {"2025-12-31T18:30"}})

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for valid T-format datetime, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Errorf("Save must be called exactly once, called %d times", saveCount)
	}
}

// TestWriterRoutes_FieldDateTimeValid_SpaceFormat verifies the human-typed
// space-separated form ("2006-01-02 15:04") also passes validation.
func TestWriterRoutes_FieldDateTimeValid_SpaceFormat(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResourceWithDateTime(
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := postWriterForm(t, p, cookieVal, url.Values{"name": {"Test"}, "starts": {"2025-12-31 18:30"}})

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for valid space-format datetime, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Errorf("Save must be called exactly once, called %d times", saveCount)
	}
}

// TestWriterRoutes_ValidateHookRejectsText verifies the per-field Validate hook
// runs for FieldText — previously a validation-free escape hatch — and blocks
// Save when it returns a non-empty message.
func TestWriterRoutes_ValidateHookRejectsText(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	r := testResource
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{
					Key: "code", Label: "Code", Kind: resource.FieldText, Required: true,
					Validate: func(val string) string {
						if val != strings.ToUpper(val) {
							return "Code must be upper-case"
						}
						return ""
					},
				},
			},
		},
		Load: func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		Save: func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	}
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	w := postWriterForm(t, p, cookieVal, url.Values{"code": {"lower"}})

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for Validate-hook rejection, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called when Validate hook rejects, called %d times", saveCount)
	}
}

// TestWriterRoutes_ValidateHookAllowsPass verifies a passing Validate hook
// (empty return) lets a valid submission through to Save.
func TestWriterRoutes_ValidateHookAllowsPass(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	r := testResource
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{
					Key: "code", Label: "Code", Kind: resource.FieldText, Required: true,
					Validate: func(val string) string {
						if val != strings.ToUpper(val) {
							return "Code must be upper-case"
						}
						return ""
					},
				},
			},
		},
		Load: func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		Save: func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	}
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	w := postWriterForm(t, p, cookieVal, url.Values{"code": {"UPPER"}})

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Errorf("Save must be called exactly once, called %d times", saveCount)
	}
}

// TestRegisterPanicsOnShortCSRFKey verifies that Register panics when CSRFKey < 32 bytes (SEC-CR-001).
func TestRegisterPanicsOnShortCSRFKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for CSRFKey shorter than 32 bytes")
		}
	}()
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     a,
		CSRFKey:  []byte("short"), // < 32 bytes — must panic
	})
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) { return nil, nil },
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	))
}

// TestRegisterPanicsOnNoSessionCookieName verifies that Register panics when the authenticator
// does not implement SessionCookieName() — fail-closed binding requirement.
func TestRegisterPanicsOnNoSessionCookieName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when authenticator lacks SessionCookieName()")
		}
	}()
	// Use a minimal auth implementation that does NOT implement SessionCookieName.
	p := resource.New(resource.Config{
		Title:    "Test Panel",
		BasePath: "/admin",
		Auth:     noSessionNameAuth{},
		CSRFKey:  testCSRFKey,
	})
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) { return nil, nil },
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	))
}

// TestWriterRoutes_EditWithIDNew returns 404 — "new" is not a valid edit id.
func TestWriterRoutes_EditWithIDNew(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, id string) (map[string]string, error) {
			// Should not be called for id="new".
			return nil, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/new/edit", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for GET /new/edit, got %d", w.Code)
	}
}

// noSessionNameAuth is a minimal auth stub that does NOT implement SessionCookieName.
type noSessionNameAuth struct{}

func (noSessionNameAuth) Require(h http.HandlerFunc) http.HandlerFunc { return h }
func (noSessionNameAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
}
func (noSessionNameAuth) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
}

// writerResourceWithOptionsFunc builds a resource with a FieldSelect backed by OptionsFunc.
func writerResourceWithOptionsFunc(
	opts func(context.Context, tenant.Tenant) ([]resource.Option, error),
	saveFn func(context.Context, tenant.Tenant, string, map[string]string) error,
) resource.Resource {
	r := testResource
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{Key: "name", Label: "Name", Kind: resource.FieldText, Required: true},
				{Key: "category", Label: "Category", Kind: resource.FieldSelect, OptionsFunc: opts},
			},
		},
		Load: func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		Save: saveFn,
	}
	return r
}

// TestWriterRoutes_OptionsFuncRendered verifies that GET /new renders options from OptionsFunc.
func TestWriterRoutes_OptionsFuncRendered(t *testing.T) {
	callCount := 0
	opts := func(_ context.Context, _ tenant.Tenant) ([]resource.Option, error) {
		callCount++
		return []resource.Option{
			{Value: "food", Label: "Food"},
			{Value: "art", Label: "Art"},
		}, nil
	}
	p := newWriterPanel()
	resource.Register(p, writerResourceWithOptionsFunc(opts,
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
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
	if !strings.Contains(body, `value="food"`) {
		t.Errorf("expected option value=food in response, body snippet: %s", body[:min(len(body), 800)])
	}
	if !strings.Contains(body, "Art") {
		t.Errorf("expected option label Art in response")
	}
	if callCount == 0 {
		t.Error("OptionsFunc was never called")
	}
}

// TestWriterRoutes_OptionsFuncPostOutOfWhitelist verifies POST with stale/invalid select value returns 422.
func TestWriterRoutes_OptionsFuncPostOutOfWhitelist(t *testing.T) {
	saveCount := 0
	// OptionsFunc returns only "food" and "art" — "deleted" is not in the fresh list.
	opts := func(_ context.Context, _ tenant.Tenant) ([]resource.Option, error) {
		return []resource.Option{
			{Value: "food", Label: "Food"},
			{Value: "art", Label: "Art"},
		}, nil
	}
	p := newWriterPanel()
	resource.Register(p, writerResourceWithOptionsFunc(opts,
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":     {"My Item"},
		"category": {"deleted"}, // not in fresh whitelist
		"_csrf":    {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for out-of-whitelist select, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called on validation error, called %d times", saveCount)
	}
}

// TestWriterRoutes_OptionsFuncError verifies that OptionsFunc error on GET returns 500.
func TestWriterRoutes_OptionsFuncError(t *testing.T) {
	opts := func(_ context.Context, _ tenant.Tenant) ([]resource.Option, error) {
		return nil, errors.New("db connection lost")
	}
	p := newWriterPanel()
	resource.Register(p, writerResourceWithOptionsFunc(opts,
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/new", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when OptionsFunc fails, got %d", w.Code)
	}
	// Must not render an empty select silently.
	if strings.Contains(w.Body.String(), `name="category"`) {
		t.Error("form must not be rendered when OptionsFunc fails")
	}
}

// TestWriterRoutes_OptionsFuncErrorOnPost verifies that OptionsFunc error on POST returns 500.
func TestWriterRoutes_OptionsFuncErrorOnPost(t *testing.T) {
	saveCount := 0
	opts := func(_ context.Context, _ tenant.Tenant) ([]resource.Option, error) {
		return nil, errors.New("db connection lost")
	}
	p := newWriterPanel()
	resource.Register(p, writerResourceWithOptionsFunc(opts,
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return nil
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{
		"name":     {"My Item"},
		"category": {"food"},
		"_csrf":    {tok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when OptionsFunc fails on POST, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Error("Save must not be called when OptionsFunc fails")
	}
}

// TestHMACAuth_OldCSRFTokenRejectedAfterRelogin verifies that a CSRF token bound to
// an old session cookie value (old nonce) is rejected when the user logs in again
// and the new cookie carries a different nonce (SEC-CR-002).
func TestHMACAuth_OldCSRFTokenRejectedAfterRelogin(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			t.Error("Save must not be called with a stale CSRF token")
			return nil
		},
	))

	// First login — capture session cookie and CSRF token.
	oldCookieVal, _ := loginAndGetCookie(t, p)
	oldTok := csrf.Issue(testCSRFKey, oldCookieVal, csrf.DefaultTTL)

	// Second login — new nonce, new cookie value.
	newCookieVal, _ := loginAndGetCookie(t, p)
	if oldCookieVal == newCookieVal {
		t.Skip("nonce collision — retry (astronomically unlikely)")
	}

	// POST using the NEW session cookie but the OLD CSRF token (bound to old nonce).
	form := url.Values{
		"name":  {"Item"},
		"_csrf": {oldTok},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: newCookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when old CSRF token used with new session cookie, got %d", w.Code)
	}
}
