package resource_test

// The delete form used to guard itself with
//   onsubmit="return confirm('Delete this { d.Resource.Title }? ...')"
// and that guard never ran once. Two independent faults in one attribute:
//
//  1. The page CSP is `script-src 'self' 'unsafe-eval'` with no
//     'unsafe-inline', so the browser drops inline event handlers. Measured in
//     Chrome: the identical button with the CSP does not fire, without it does.
//     The failure is OPEN — no handler, no prompt, form submits, row gone.
//  2. `{ d.Resource.Title }` inside a quoted attribute is not a templ
//     expression, so it reached the browser verbatim. That literal is the
//     proof of fault 1: had the dialog ever been shown to a human, someone
//     would have read "Delete this { d.Resource.Title }?" and said something.
//
// Both are asserted here, because fixing either alone still ships a delete
// button that either does not ask or asks nonsense.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

var inlineHandlerRe = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)

func TestDeleteFormConfirmSurvivesCSP(t *testing.T) {
	p := newWriterPanel()
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	)
	r.Detailer = func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "S", Items: []resource.DetailItem{{Label: "id", Value: id}}}}, nil
	}
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error { return nil }
	resource.Register(p, r)

	cookieVal, _ := loginAndGetCookie(t, p)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detail page: status %d", w.Code)
	}
	body := w.Body.String()

	// The delete form has to be on the page at all, or everything below is
	// asserted over nothing.
	i := strings.Index(body, "/42/delete")
	if i < 0 {
		t.Fatal("no delete form on the detail page — the rest of this test would pass vacuously")
	}
	start := strings.LastIndex(body[:i], "<form")
	end := strings.Index(body[i:], ">")
	if start < 0 || end < 0 {
		t.Fatal("could not isolate the delete form's opening tag")
	}
	tag := body[start : i+end+1]

	// 1. No inline handler of any kind: CSP drops them, and it drops them
	//    silently and open.
	if m := inlineHandlerRe.FindString(tag); m != "" {
		t.Errorf("delete form carries an inline handler (%q). The admin CSP is "+
			"script-src 'self' 'unsafe-eval' with no 'unsafe-inline', so it never "+
			"runs and the form submits unconfirmed", strings.TrimSpace(m))
	}

	// 2. The confirmation text is present AND interpolated.
	if !strings.Contains(tag, "data-confirm=") {
		t.Fatalf("delete form has no data-confirm attribute — nothing gates it:\n%s", tag)
	}
	if strings.Contains(tag, "d.Resource.Title") || strings.Contains(tag, "{ ") {
		t.Errorf("confirmation text was not interpolated — a templ expression inside a "+
			"quoted attribute is literal text:\n%s", tag)
	}
	// writerResource's title, so the assertion names the value rather than
	// merely checking something non-empty landed there.
	if !strings.Contains(tag, "Delete this Items?") {
		t.Errorf("confirmation text does not name the resource; got:\n%s", tag)
	}
}
