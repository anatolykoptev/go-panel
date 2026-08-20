package resource_test

// The delete form on the detail page rendered its hidden input as
// name="csrf_token" while Panel.verifyCSRFToken reads csrf.FormField ("_csrf").
// So the form posted a field nothing looked at, FormValue returned "", and
// every delete answered 403. Measured against production on 2026-08-20:
//
//	POST …/delete with csrf_token=<token>  -> 403 invalid CSRF token
//	POST …/delete with _csrf=<same token>  -> 500 delete failed (reached the store)
//
// The button rendered, the confirm dialog ran, and it had never deleted
// anything. A test that asserts a field NAME cannot catch this — it would just
// encode whichever name its author believed in. This one submits the form the
// page actually renders.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

var (
	deleteFormRe  = regexp.MustCompile(`(?s)<form[^>]*/delete"[^>]*>(.*?)</form>`)
	hiddenInputRe = regexp.MustCompile(`<input[^>]*type="hidden"[^>]*>`)
	attrRe        = regexp.MustCompile(`(\w[\w-]*)="([^"]*)"`)
)

// Falsification: change resource/detail.templ's hidden input back to
// name="csrf_token" and this goes RED with 403 — the exact production symptom.
func TestDeleteForm_SubmitsWhatThePageRenders(t *testing.T) {
	p := newWriterPanel()
	called := false
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	)
	r.Detailer = func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "S", Items: []resource.DetailItem{{Label: "id", Value: id}}}}, nil
	}
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error {
		called = true
		return nil
	}
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	// 1. Render the detail page and take the delete form as the browser sees it.
	get := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items/42", nil)
	get.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	gw := httptest.NewRecorder()
	p.Handler().ServeHTTP(gw, get)
	if gw.Code != http.StatusOK {
		t.Fatalf("detail page: status %d", gw.Code)
	}
	form := deleteFormRe.FindStringSubmatch(gw.Body.String())
	if form == nil {
		t.Fatal("no delete form on the detail page — the submission below would prove nothing")
	}

	// 2. Collect its hidden inputs verbatim. Nothing here names a field: the
	//    page decides what is posted, which is the whole point.
	values := url.Values{}
	for _, input := range hiddenInputRe.FindAllString(form[1], -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(input, -1) {
			attrs[m[1]] = m[2]
		}
		if n := attrs["name"]; n != "" {
			values.Set(n, attrs["value"])
		}
	}
	if len(values) == 0 {
		t.Fatal("the delete form carries no hidden inputs at all — nothing to submit")
	}
	t.Logf("submitting the fields the page rendered: %v", fieldNames(values))

	// 3. Submit exactly that.
	post := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/42/delete", strings.NewReader(values.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	pw := httptest.NewRecorder()
	p.Handler().ServeHTTP(pw, post)

	if pw.Code == http.StatusForbidden {
		t.Fatalf("the form the page renders is rejected as a CSRF failure (403). The hidden "+
			"input's name does not match the field verifyCSRFToken reads, so this button "+
			"cannot ever delete. Fields submitted: %v", fieldNames(values))
	}
	if !called {
		t.Errorf("Writer.Delete was never called (status %d) — the form reached the route "+
			"but not the store", pw.Code)
	}
}

func fieldNames(v url.Values) []string {
	out := make([]string, 0, len(v))
	for k := range v {
		out = append(out, k)
	}
	return out
}
