// views_wiring_test.go — the WIRING half of Resource.Views. views_internal_test.go
// pins the pieces (resolveView, the chip classes, the validator); this pins that
// the list handler actually calls them.
//
// The class it exists to prevent: a primitive that is implemented, tested, and
// never reached — the Lister keeps receiving an empty View while the chips
// render and do nothing, and every unit test stays green.
package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// viewsResource is a resource with three period views and a Lister that records
// the ListQuery it was handed.
func viewsResource(got *resource.ListQuery) resource.Resource {
	r := minimalResource("revenue")
	r.Views = []resource.View{
		{Key: "month", Label: "This month"},
		{Key: "week", Label: "This week"},
		{Key: "quarter", Label: "This quarter"},
	}
	r.ViewsLabel = "period"
	r.Lister = func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		*got = q
		return []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "row"}}}}, 1, nil
	}
	return r
}

func getAuthed(t *testing.T, p *resource.Panel, a *auth.HMACAuth, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: authCookie(t, a, "admin", "secret")})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// TestListHandler_PassesTheResolvedViewToTheLister — without this the whole
// primitive is inert: chips render, the URL changes, and the Lister never learns
// which one is on.
//
// Falsification: in makeListHandler, drop `View: view` from the ListQuery
// literal → RED.
func TestListHandler_PassesTheResolvedViewToTheLister(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"/admin/revenue?view=quarter", "quarter"},
		{"/admin/revenue?view=week", "week"},
		{"/admin/revenue", "month"},             // no param → first declared view
		{"/admin/revenue?view=decade", "month"}, // unknown → first declared view, never the raw value
	} {
		var got resource.ListQuery
		a := newTestAuth()
		p := panelWithAuth(a)
		resource.Register(p, viewsResource(&got))

		if w := getAuthed(t, p, a, tc.url); w.Code != http.StatusOK {
			t.Fatalf("%s: got %d, want 200", tc.url, w.Code)
		}
		if got.View != tc.want {
			t.Errorf("%s: Lister received View %q, want %q", tc.url, got.View, tc.want)
		}
	}
}

// TestListHandler_MarksTheSelectedViewChip — the handler must hand the template
// the resolved view AND the request's values, or the bar renders with nothing
// selected and the operator cannot see which period they are looking at.
//
// Falsification: in makeListHandler, drop `ActiveView: view` from the
// listPageData literal → RED. Drop `Selected: q` → the search-value test in
// views_internal_test.go stays green but a filter chip stops highlighting, so
// this asserts the view chip specifically.
func TestListHandler_MarksTheSelectedViewChip(t *testing.T) {
	var got resource.ListQuery
	a := newTestAuth()
	p := panelWithAuth(a)
	resource.Register(p, viewsResource(&got))

	body := getAuthed(t, p, a, "/admin/revenue?view=quarter").Body.String()
	re := regexp.MustCompile(`(?s)<button[^>]*?name="view"[^>]*?value="quarter"[^>]*?class="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no view chip for quarter in the rendered page")
	}
	if m[1] != "filter-chip active" {
		t.Errorf("the selected view chip has class %q, want \"filter-chip active\"", m[1])
	}
	// And a different one must NOT be active.
	re2 := regexp.MustCompile(`(?s)<button[^>]*?name="view"[^>]*?value="week"[^>]*?class="([^"]*)"`)
	if m2 := re2.FindStringSubmatch(body); m2 == nil || m2[1] != "filter-chip" {
		t.Errorf("the unselected view chip has class %v, want \"filter-chip\"", m2)
	}
}

// TestRegister_PanicsOnADuplicateViewKey pins the validator to Register itself.
// Calling validateViewsConfig directly would pass even if Register never invoked
// it — the guard would exist and never run.
//
// Falsification: delete the validateViewsConfig(r) call in Register → RED.
func TestRegister_PanicsOnADuplicateViewKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Register accepted a duplicate view Key — the second chip would be unreachable with no error anywhere")
		}
	}()
	a := newTestAuth()
	p := panelWithAuth(a)
	r := minimalResource("revenue")
	r.Views = []resource.View{{Key: "month"}, {Key: "month"}}
	resource.Register(p, r)
}

// TestListHandler_NoViewsLeavesTheQueryEmpty — the default path must be
// untouched: a resource that declares no views gets View == "" and no chips, so
// every existing consumer is unaffected.
//
// Falsification: in resolveView, return a non-empty default for the no-Views
// case → RED.
func TestListHandler_NoViewsLeavesTheQueryEmpty(t *testing.T) {
	var got resource.ListQuery
	a := newTestAuth()
	p := panelWithAuth(a)
	r := minimalResource("places")
	r.Lister = func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		got = q
		return nil, 0, nil
	}
	resource.Register(p, r)

	body := getAuthed(t, p, a, "/admin/places?view=week").Body.String()
	if got.View != "" {
		t.Errorf("a resource with no Views received View %q, want the empty string", got.View)
	}
	if regexp.MustCompile(`name="view"`).MatchString(body) {
		t.Error("a resource with no Views rendered view chips")
	}
}
