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
	"github.com/anatolykoptev/go-panel/locale"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// mustSet builds a validated locale.Set or fails the test.
func mustSet(t *testing.T, def locale.Locale, available ...locale.Locale) locale.Set {
	t.Helper()
	s, err := locale.NewSet(def, available...)
	if err != nil {
		t.Fatalf("locale.NewSet(%q, %v): %v", def, available, err)
	}
	return s
}

// newLocalePanel builds a writer-capable Panel configured with the given locale set.
func newLocalePanel(t *testing.T, set locale.Set) *resource.Panel {
	t.Helper()
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
		Locales:  set,
	})
}

// localeWriterResource builds a resource whose form mixes a shared
// (non-translatable) required field with translatable fields, exercising the
// per-locale field split. "slug" is shared (edited on Default); "title" and
// "body" are translatable.
func localeWriterResource(
	loadFn func(context.Context, tenant.Tenant, string) (map[string]string, error),
	saveFn func(context.Context, tenant.Tenant, string, map[string]string) error,
) resource.Resource {
	r := testResource
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{
			Fields: []resource.Field{
				{Key: "slug", Label: "Slug", Kind: resource.FieldText, Required: true}, // shared
				{Key: "title", Label: "Title", Kind: resource.FieldText, Required: true, Translatable: true},
				{Key: "body", Label: "Body", Kind: resource.FieldTextarea, Translatable: true},
			},
		},
		Load: loadFn,
		Save: saveFn,
	}
	return r
}

// loadAll returns every field populated — a stand-in for an existing record.
func loadAll(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
	return map[string]string{"slug": "my-slug", "title": "My Title", "body": "Body text"}, nil
}

// noopSave is a Save that records nothing.
func noopSave(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil }

// getForm issues an authenticated GET and returns the recorder.
func getForm(t *testing.T, p *resource.Panel, cookieVal, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// postSave issues an authenticated, CSRF-valid POST to a save path.
func postSave(t *testing.T, p *resource.Panel, cookieVal, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("_csrf", csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// TestLocale_EditSwitcherRendersOnMultiLocale verifies the locale switcher is
// rendered on a multi-locale edit form, with a tab per configured locale.
func TestLocale_EditSwitcherRendersOnMultiLocale(t *testing.T) {
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, noopSave))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := getForm(t, p, cookieVal, "/admin/items/1/edit")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `role="tablist"`) {
		t.Errorf("expected a locale switcher (role=tablist) on multi-locale edit")
	}
	if !strings.Contains(body, `/admin/items/1/edit?locale=es`) {
		t.Errorf("expected an ES tab linking to ?locale=es")
	}
	// EN is active (a span), ES is a link.
	if !strings.Contains(body, `aria-selected="true"`) {
		t.Errorf("expected the active (EN) tab marked aria-selected=true")
	}
}

// TestLocale_SingleLocaleNoSwitcher verifies an explicit single-locale set
// renders no switcher and shows every field on edit.
func TestLocale_SingleLocaleNoSwitcher(t *testing.T) {
	p := newLocalePanel(t, mustSet(t, "en", "en"))
	resource.Register(p, localeWriterResource(loadAll, noopSave))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := getForm(t, p, cookieVal, "/admin/items/1/edit")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `role="tablist"`) {
		t.Errorf("single-locale edit must not render a locale switcher")
	}
	for _, name := range []string{`name="slug"`, `name="title"`, `name="body"`} {
		if !strings.Contains(body, name) {
			t.Errorf("single-locale edit must render every field; missing %s", name)
		}
	}
}

// TestLocale_NewFormNoSwitcherAllFields verifies that create (GET /new) shows no
// switcher even on a multi-locale panel and renders every field (create always
// lands in the Default locale).
func TestLocale_NewFormNoSwitcherAllFields(t *testing.T) {
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, noopSave))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := getForm(t, p, cookieVal, "/admin/items/new")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `role="tablist"`) {
		t.Errorf("create form must not render a locale switcher")
	}
	for _, name := range []string{`name="slug"`, `name="title"`, `name="body"`} {
		if !strings.Contains(body, name) {
			t.Errorf("create form must render every field; missing %s", name)
		}
	}
	// Action must NOT carry a ?locale= query (create is Default).
	if strings.Contains(body, `/items/new/save?locale=`) {
		t.Errorf("create save action must not carry ?locale=")
	}
}

// TestLocale_SecondaryLocaleRendersOnlyTranslatable verifies that editing a
// secondary locale renders only translatable fields (shared fields hidden).
func TestLocale_SecondaryLocaleRendersOnlyTranslatable(t *testing.T) {
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, noopSave))
	cookieVal, _ := loginAndGetCookie(t, p)

	w := getForm(t, p, cookieVal, "/admin/items/1/edit?locale=es")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="title"`) || !strings.Contains(body, `name="body"`) {
		t.Errorf("secondary-locale edit must render translatable fields (title, body)")
	}
	if strings.Contains(body, `name="slug"`) {
		t.Errorf("secondary-locale edit must NOT render the shared field (slug)")
	}
	if !strings.Contains(body, "translation") {
		t.Errorf("secondary-locale edit should show the translation hint")
	}
}

// TestLocale_EditLoadReceivesLocaleFromCtx verifies the active locale flows to
// Writer.Load via context: default when no query, the secondary code with ?locale=.
func TestLocale_EditLoadReceivesLocaleFromCtx(t *testing.T) {
	var loadLoc locale.Locale
	capture := func(ctx context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
		loadLoc = locale.From(ctx)
		return map[string]string{"slug": "s", "title": "t", "body": "b"}, nil
	}
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(capture, noopSave))
	cookieVal, _ := loginAndGetCookie(t, p)

	if w := getForm(t, p, cookieVal, "/admin/items/1/edit"); w.Code != http.StatusOK {
		t.Fatalf("default-locale edit: expected 200, got %d", w.Code)
	}
	if loadLoc != "en" {
		t.Errorf("Load ctx locale = %q, want default %q", loadLoc, "en")
	}

	if w := getForm(t, p, cookieVal, "/admin/items/1/edit?locale=es"); w.Code != http.StatusOK {
		t.Fatalf("secondary-locale edit: expected 200, got %d", w.Code)
	}
	if loadLoc != "es" {
		t.Errorf("Load ctx locale = %q, want %q", loadLoc, "es")
	}

	// An unknown locale resolves to the Default (not honoured blindly).
	if w := getForm(t, p, cookieVal, "/admin/items/1/edit?locale=zz"); w.Code != http.StatusOK {
		t.Fatalf("unknown-locale edit: expected 200, got %d", w.Code)
	}
	if loadLoc != "en" {
		t.Errorf("Load ctx locale for unknown ?locale=zz = %q, want default %q", loadLoc, "en")
	}
}

// TestLocale_SaveSecondaryLocaleValidatesOnlyTranslatable verifies that a save on
// a secondary locale (a) does not require the shared field, (b) hands Save the
// secondary locale via context, and (c) collects only translatable values.
func TestLocale_SaveSecondaryLocaleValidatesOnlyTranslatable(t *testing.T) {
	var (
		saveLoc    locale.Locale
		saveID     string
		saveValues map[string]string
		saveCount  int
	)
	save := func(ctx context.Context, _ tenant.Tenant, id string, values map[string]string) error {
		saveCount++
		saveLoc = locale.From(ctx)
		saveID = id
		saveValues = values
		return nil
	}
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, save))
	cookieVal, _ := loginAndGetCookie(t, p)

	// slug (shared, required) is deliberately POSTed but must be ignored on es.
	form := url.Values{"title": {"Título"}, "body": {"Cuerpo"}, "slug": {"should-be-dropped"}}
	w := postSave(t, p, cookieVal, "/admin/items/1/save?locale=es", form)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 (shared field not spuriously required), got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Fatalf("Save called %d times, want 1", saveCount)
	}
	if saveLoc != "es" {
		t.Errorf("Save ctx locale = %q, want %q", saveLoc, "es")
	}
	if saveID != "1" {
		t.Errorf("Save id = %q, want %q", saveID, "1")
	}
	if saveValues["title"] != "Título" || saveValues["body"] != "Cuerpo" {
		t.Errorf("Save values missing translatable fields: %v", saveValues)
	}
	if _, ok := saveValues["slug"]; ok {
		t.Errorf("Save on a secondary locale must NOT collect the shared field; got slug=%q", saveValues["slug"])
	}
}

// TestLocale_SaveSecondaryLocaleMissingRequiredTranslatable verifies a required
// translatable field is still enforced on a secondary locale.
func TestLocale_SaveSecondaryLocaleMissingRequiredTranslatable(t *testing.T) {
	saveCount := 0
	save := func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
		saveCount++
		return nil
	}
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, save))
	cookieVal, _ := loginAndGetCookie(t, p)

	// title (translatable, required) omitted.
	w := postSave(t, p, cookieVal, "/admin/items/1/save?locale=es", url.Values{"body": {"Cuerpo"}})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for missing required translatable field, got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called on validation error, called %d", saveCount)
	}
	if !strings.Contains(w.Body.String(), "required") {
		t.Errorf("expected 'required' in 422 body")
	}
}

// TestLocale_SaveDefaultLocaleValidatesAllFields verifies the shared field IS
// required when saving on the Default locale.
func TestLocale_SaveDefaultLocaleValidatesAllFields(t *testing.T) {
	saveCount := 0
	save := func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
		saveCount++
		return nil
	}
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, save))
	cookieVal, _ := loginAndGetCookie(t, p)

	// slug (shared, required) omitted on the default locale → must 422.
	w := postSave(t, p, cookieVal, "/admin/items/1/save?locale=en", url.Values{"title": {"Title"}, "body": {"Body"}})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 (shared field required on Default), got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called on validation error, called %d", saveCount)
	}
}

// TestLocale_CreateForcesDefaultLocale verifies create ignores ?locale= and
// persists in the Default locale (you cannot translate a record that does not
// exist yet).
func TestLocale_CreateForcesDefaultLocale(t *testing.T) {
	var (
		saveLoc   locale.Locale
		saveID    string
		saveCount int
	)
	save := func(ctx context.Context, _ tenant.Tenant, id string, _ map[string]string) error {
		saveCount++
		saveLoc = locale.From(ctx)
		saveID = id
		return nil
	}
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, save))
	cookieVal, _ := loginAndGetCookie(t, p)

	// All fields supplied; ?locale=es must be ignored for create.
	form := url.Values{"slug": {"new-slug"}, "title": {"Title"}, "body": {"Body"}}
	w := postSave(t, p, cookieVal, "/admin/items/new/save?locale=es", form)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Fatalf("Save called %d times, want 1", saveCount)
	}
	if saveLoc != "en" {
		t.Errorf("create Save ctx locale = %q, want Default %q (?locale=es must be ignored)", saveLoc, "en")
	}
	if saveID != "" {
		t.Errorf("create Save id = %q, want empty", saveID)
	}
}

// TestLocale_CreateValidatesAllFieldsIgnoringLocaleQuery verifies create
// validates the full field set (shared required field enforced) regardless of a
// ?locale= query that would otherwise relax it.
func TestLocale_CreateValidatesAllFieldsIgnoringLocaleQuery(t *testing.T) {
	saveCount := 0
	save := func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
		saveCount++
		return nil
	}
	p := newLocalePanel(t, mustSet(t, "en", "en", "es"))
	resource.Register(p, localeWriterResource(loadAll, save))
	cookieVal, _ := loginAndGetCookie(t, p)

	// slug omitted; ?locale=es would relax it on EDIT, but create forces Default.
	w := postSave(t, p, cookieVal, "/admin/items/new/save?locale=es", url.Values{"title": {"Title"}, "body": {"Body"}})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 (shared field required on create), got %d", w.Code)
	}
	if saveCount != 0 {
		t.Errorf("Save must not be called on validation error, called %d", saveCount)
	}
}
