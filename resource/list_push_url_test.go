package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// pushURLResource has views, a search filter, a sortable column and enough
// rows for three pages, so every push and href site renders.
func pushURLResource() resource.Resource {
	rows := make([]resource.Row, 5)
	for i := range rows {
		rows[i] = resource.Row{ID: string(rune('a' + i)), Cells: []resource.Cell{{Value: "n"}, {Value: "s"}}}
	}
	return resource.Resource{
		Name: "leads", Title: "Leads", Icon: "*",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Label: "Name", SQLExpr: "name", Sortable: true},
				{Key: "status", Label: "Status", SQLExpr: "status"},
			},
			DefaultKey: "name", DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "q", SQLExprs: []string{"name"}, Match: admintable.ILike},
		}},
		Views: []resource.View{{Key: "open", Label: "Open"}, {Key: "all", Label: "All"}},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return rows, 100, nil // 100 total at default page size: several pages
		},
	}
}

func pushURLGet(t *testing.T, target string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw",
		HMACKey: []byte("0123456789abcdef0123456789abcdef"), BasePath: "/admin"})
	p := resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a})
	resource.Register(p, pushURLResource())
	lreq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login",
		strings.NewReader("username=op&password=pw"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lrec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(lrec, lreq)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	for _, c := range lrec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	return rec
}

// TestRows_HTMXPushesPageURL: the fragment answer names the list PAGE as the
// URL to push. Without it htmx pushes the /rows request URL and the address
// bar reloads as a bare, layout-less table.
func TestRows_HTMXPushesPageURL(t *testing.T) {
	rec := pushURLGet(t, "/admin/leads/rows?view=all&q=x", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got, want := rec.Header().Get("HX-Push-Url"), "/admin/leads?q=x&view=all"; got != want {
		t.Errorf("HX-Push-Url = %q, want %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), `id="leads-region"`) {
		t.Errorf("htmx request must still get the fragment")
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("fragment must not carry the layout")
	}
}

// TestRows_LoadMoreDoesNotPush: append=1 swaps rows in place; pushing a URL
// would move the address bar to a page the user did not navigate to.
func TestRows_LoadMoreDoesNotPush(t *testing.T) {
	rec := pushURLGet(t, "/admin/leads/rows?page=2&append=1", true)
	if got := rec.Header().Get("HX-Push-Url"); got != "" {
		t.Errorf("HX-Push-Url = %q on Load-more, want none", got)
	}
}

// TestRows_PlainRequestRedirectsToPage: an already-shared /rows link, a
// bookmark or a middle-click lands on the full page with the same query.
func TestRows_PlainRequestRedirectsToPage(t *testing.T) {
	rec := pushURLGet(t, "/admin/leads/rows?view=x&q=y", false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), "/admin/leads?q=y&view=x"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if v := rec.Header().Get("Vary"); !strings.Contains(v, "HX-Request") {
		t.Errorf("Vary = %q, want HX-Request", v)
	}
}

// TestRows_PlainRequestNoQuery: no query, no dangling "?".
func TestRows_PlainRequestNoQuery(t *testing.T) {
	rec := pushURLGet(t, "/admin/leads/rows", false)
	if got := rec.Header().Get("Location"); got != "/admin/leads" {
		t.Errorf("Location = %q, want /admin/leads", got)
	}
}

// TestListPage_ReadsViewFromQuery: the page a push or redirect lands on
// renders with the filter applied, chip active and input pre-filled.
func TestListPage_ReadsViewFromQuery(t *testing.T) {
	body := pushURLGet(t, "/admin/leads?view=all&q=zed&page=2", false).Body.String()
	if !regexp.MustCompile(`class="filter-chip active"[^>]*>All<`).MatchString(body) {
		t.Errorf("view=all chip not active")
	}
	if regexp.MustCompile(`class="filter-chip active"[^>]*>Open<`).MatchString(body) {
		t.Errorf("view=open chip active on view=all")
	}
	if !regexp.MustCompile(`<input[^>]*name="q"[^>]*value="zed"`).MatchString(body) {
		t.Errorf("search input not pre-filled")
	}
}

var hrefAttr = regexp.MustCompile(`\bhref="([^"]*)"`)

// TestList_NoHrefPointsAtRows: derived from the rendered output so a new
// anchor site cannot slip a /rows href in. hx-get may (and must) still use it.
func TestList_NoHrefPointsAtRows(t *testing.T) {
	for _, target := range []string{"/admin/leads", "/admin/leads?page=2&view=all"} {
		body := pushURLGet(t, target, false).Body.String()
		hrefs := hrefAttr.FindAllStringSubmatch(body, -1)
		var sortOrPage int
		for _, m := range hrefs {
			if strings.Contains(m[1], "/rows") {
				t.Errorf("%s: href %q points at the /rows fragment", target, m[1])
			}
			if strings.Contains(m[1], "sort=") || strings.Contains(m[1], "page=") {
				sortOrPage++
			}
		}
		if sortOrPage == 0 {
			t.Errorf("%s: rendered no sort/pagination hrefs; test is vacuous", target)
		}
		if !strings.Contains(body, `hx-get="/admin/leads/rows`) {
			t.Errorf("%s: in-place swap must still hx-get /rows", target)
		}
	}
}
