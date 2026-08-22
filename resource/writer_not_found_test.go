package resource_test

// A Writer had no way to say "that id names no row". Every Load/Save/Delete/
// Restore error became a 500, so a stale /edit link told the operator the
// server had broken — the same wrong-status class the detail route fixed long
// ago, on the four routes beside it.
//
// Measured on a consumer: go-grad's native places edit route answered 500 for
// an id that is not a bigint, because the type error travelled out of Load.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// notFoundWriter builds a resource whose every write closure reports the id as
// absent. One resource, four routes: the point is that the sentinel is honoured
// on all of them, not on whichever one was remembered.
func notFoundWriter() resource.Resource {
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return nil, resource.ErrDetailNotFound
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return resource.ErrDetailNotFound
		},
	)
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error {
		return resource.ErrDetailNotFound
	}
	r.Writer.Restore = func(_ context.Context, _ tenant.Tenant, _ string) error {
		return resource.ErrDetailNotFound
	}
	return r
}

func postForm(t *testing.T, p *resource.Panel, cookieVal, path string, form url.Values) *httptest.ResponseRecorder {
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

// TestWriterRoutes_ErrDetailNotFoundIs404 is the gate.
//
// RED-on-revert: drop any one of the four errors.Is(…, ErrDetailNotFound)
// branches in resource.go (editFormHandler, handleSaveError, handleDeleteError,
// restoreHandler) and that subtest returns 500 again.
func TestWriterRoutes_ErrDetailNotFoundIs404(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, notFoundWriter())
	cookieVal, _ := loginAndGetCookie(t, p)

	t.Run("edit", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/999/edit", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET /items/999/edit: got %d, want 404 (body: %s)", w.Code, w.Body.String())
		}
	})

	t.Run("save", func(t *testing.T) {
		w := postForm(t, p, cookieVal, "/admin/items/999/save", url.Values{
			"name": {"whatever"}, "status": {"active"}, "note": {"{}"},
		})
		if w.Code != http.StatusNotFound {
			t.Errorf("POST /items/999/save: got %d, want 404 (body: %s)", w.Code, w.Body.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		w := postForm(t, p, cookieVal, "/admin/items/999/delete", url.Values{})
		if w.Code != http.StatusNotFound {
			t.Errorf("POST /items/999/delete: got %d, want 404 (body: %s)", w.Code, w.Body.String())
		}
	})

	t.Run("restore", func(t *testing.T) {
		w := postForm(t, p, cookieVal, "/admin/items/999/restore", url.Values{})
		if w.Code != http.StatusNotFound {
			t.Errorf("POST /items/999/restore: got %d, want 404 (body: %s)", w.Code, w.Body.String())
		}
	})
}

// TestWriterRoutes_OtherErrorsStay500 is the other half, and the one that keeps
// this change additive. A store that is genuinely broken must NOT be reported
// as a missing row: that would turn every outage into a quiet 404 and hide it
// from whatever watches for 5xx.
func TestWriterRoutes_OtherErrorsStay500(t *testing.T) {
	boom := errors.New("the database is on fire")
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) { return nil, boom },
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return boom },
	)
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error { return boom }
	r.Writer.Restore = func(_ context.Context, _ tenant.Tenant, _ string) error { return boom }

	p := newWriterPanel()
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	t.Run("edit", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/999/edit", nil)
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("GET /items/999/edit with a real failure: got %d, want 500", w.Code)
		}
	})
	for _, tc := range []struct{ name, path string }{
		{"save", "/admin/items/999/save"},
		{"delete", "/admin/items/999/delete"},
		{"restore", "/admin/items/999/restore"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.name == "save" {
				form = url.Values{"name": {"whatever"}, "status": {"active"}, "note": {"{}"}}
			}
			w := postForm(t, p, cookieVal, tc.path, form)
			if w.Code != http.StatusInternalServerError {
				t.Errorf("POST %s with a real failure: got %d, want 500 (body: %s)", tc.path, w.Code, w.Body.String())
			}
		})
	}
}

// The hooks still fire on a not-found. They are told what happened either way,
// and a consumer that counts failures there must not lose the count because the
// status changed.
func TestWriterRoutes_NotFoundStillCallsTheHooks(t *testing.T) {
	var afterSave, afterDelete error
	var savedCalled, deletedCalled bool
	r := notFoundWriter()
	r.Writer.AfterSave = func(_ context.Context, _ string, err error) { savedCalled, afterSave = true, err }
	r.Writer.AfterDelete = func(_ context.Context, _ string, err error) { deletedCalled, afterDelete = true, err }

	p := newWriterPanel()
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	postForm(t, p, cookieVal, "/admin/items/999/save", url.Values{
		"name": {"whatever"}, "status": {"active"}, "note": {"{}"},
	})
	postForm(t, p, cookieVal, "/admin/items/999/delete", url.Values{})

	if !savedCalled || !errors.Is(afterSave, resource.ErrDetailNotFound) {
		t.Errorf("AfterSave called=%v err=%v — want it called with ErrDetailNotFound", savedCalled, afterSave)
	}
	if !deletedCalled || !errors.Is(afterDelete, resource.ErrDetailNotFound) {
		t.Errorf("AfterDelete called=%v err=%v — want it called with ErrDetailNotFound", deletedCalled, afterDelete)
	}
}
