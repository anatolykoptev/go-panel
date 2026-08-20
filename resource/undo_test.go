package resource_test

// Delete-with-undo. The property that carries the most weight here is not that
// undo works — it is that the one-click row Delete appears ONLY where an undo
// exists. Gate it on Writer.Delete instead and every consumer that has a delete
// (go-job has four) silently gains a one-click destructive button in its lists.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

func undoResource(t *testing.T, withRestore bool, onRestore func(string)) resource.Resource {
	t.Helper()
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	)
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error { return nil }
	if withRestore {
		r.Writer.Restore = func(_ context.Context, _ tenant.Tenant, id string) error {
			if onRestore != nil {
				onRestore(id)
			}
			return nil
		}
	}
	return r
}

func getList(t *testing.T, p *resource.Panel, cookieVal, path string) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, w.Code)
	}
	return w.Body.String()
}

// Falsification: gate the button in resource/list.templ on Writer.Delete
// instead of Writer.Restore and the second half goes RED.
func TestRowDelete_OnlyWhereDeleteCanBeUndone(t *testing.T) {
	t.Run("with Restore the row offers Delete", func(t *testing.T) {
		p := newWriterPanel()
		resource.Register(p, undoResource(t, true, nil))
		cookieVal, _ := loginAndGetCookie(t, p)
		body := getList(t, p, cookieVal, "/admin/items")
		if !strings.Contains(body, `class="row-delete"`) {
			t.Error("a resource that can restore does not offer a row-level Delete")
		}
		if !strings.Contains(body, "/delete") {
			t.Error("the button posts nowhere")
		}
	})

	t.Run("without Restore the row does NOT", func(t *testing.T) {
		p := newWriterPanel()
		resource.Register(p, undoResource(t, false, nil))
		cookieVal, _ := loginAndGetCookie(t, p)
		body := getList(t, p, cookieVal, "/admin/items")
		if strings.Contains(body, `class="row-delete"`) {
			t.Error("a resource with NO way back gained a one-click delete in its list; " +
				"every consumer with a Delete would get this, which is the blast radius " +
				"this gate exists to prevent")
		}
	})
}

// A delete that came from the table swaps the row for its dimmed placeholder
// instead of redirecting, or the operator loses their place in the list.
func TestDelete_FromTable_SwapsTheRowForAnUndo(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, undoResource(t, true, nil))
	cookieVal, _ := loginAndGetCookie(t, p)
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)

	form := url.Values{csrf.FormField: {tok}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/42/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`class="row-deleted"`, `data-undo-after=`, `class="row-undo"`, "/42/restore"} {
		if !strings.Contains(body, want) {
			t.Errorf("the swapped row is missing %q; got:\n%s", want, body)
		}
	}
	if w.Header().Get("HX-Redirect") != "" {
		t.Error("the table delete redirected instead of swapping the row in place")
	}
}

// Restore is reachable, guarded, and actually calls the consumer's hook.
func TestRestore_RunsAndRefreshes(t *testing.T) {
	restored := ""
	p := newWriterPanel()
	resource.Register(p, undoResource(t, true, func(id string) { restored = id }))
	cookieVal, _ := loginAndGetCookie(t, p)
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)

	form := url.Values{csrf.FormField: {tok}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/42/restore", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body: %s", w.Code, w.Body.String())
	}
	if restored != "42" {
		t.Errorf("Writer.Restore called with %q, want \"42\"", restored)
	}
	if w.Header().Get("HX-Refresh") != "true" {
		t.Error("no HX-Refresh: the restored row would not reappear until the operator " +
			"reloaded by hand")
	}
}

func TestRestore_NotMountedWithoutRestore(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, undoResource(t, false, nil))
	cookieVal, _ := loginAndGetCookie(t, p)
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{csrf.FormField: {tok}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/42/restore", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("restore answered %d without a Restore hook; a route with nothing behind "+
			"it is worse than no route", w.Code)
	}
}

func TestRestore_RequiresCSRF(t *testing.T) {
	called := false
	p := newWriterPanel()
	resource.Register(p, undoResource(t, true, func(string) { called = true }))
	cookieVal, _ := loginAndGetCookie(t, p)
	form := url.Values{csrf.FormField: {"not-a-token"}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/42/restore", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("restore without a valid token: %d, want 403", w.Code)
	}
	if called {
		t.Error("Writer.Restore ran despite a rejected CSRF token")
	}
}

// A delete from the DETAIL page has no row to swap, so it hands the list the id
// and the list offers the same way back as a toast — server-rendered, so it
// survives a reload and needs no script.
func TestDelete_FromDetail_LeavesAnUndoToastOnTheList(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, undoResource(t, true, nil))
	cookieVal, _ := loginAndGetCookie(t, p)
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)

	form := url.Values{csrf.FormField: {tok}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/42/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("detail delete: status %d, want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "restorable=42") {
		t.Fatalf("redirect %q does not name the deleted row, so the list cannot offer undo", loc)
	}

	body := getList(t, p, cookieVal, loc)
	for _, want := range []string{`class="undo-toast"`, "/42/restore", "Вернуть"} {
		if !strings.Contains(body, want) {
			t.Errorf("the list is missing %q from the undo toast", want)
		}
	}
}

// The parameter reaches a URL in the rendered toast, so junk must not.
func TestRestorableParam_RejectsAnythingButAnID(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, undoResource(t, true, nil))
	cookieVal, _ := loginAndGetCookie(t, p)
	for _, bad := range []string{`"><script>`, "../../etc", "a b", strings.Repeat("9", 65)} {
		body := getList(t, p, cookieVal, "/admin/items?restorable="+url.QueryEscape(bad))
		if strings.Contains(body, `class="undo-toast"`) {
			t.Errorf("a toast was rendered for restorable=%q", bad)
		}
	}
	// and a real one still works, so the check above is not just refusing everything
	if body := getList(t, p, cookieVal, "/admin/items?restorable=42"); !strings.Contains(body, `class="undo-toast"`) {
		t.Error("a well-formed id was rejected too — the guard refuses everything")
	}
}
