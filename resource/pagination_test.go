package resource_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// pagedResource serves 120 synthetic rows honoring Limit/Offset — 3 pages at
// the default page size of 50.
var pagedResource = resource.Resource{
	Name:  "paged",
	Title: "Paged",
	Icon:  "📄",
	Group: "Content",
	Sort: admintable.Spec{
		Columns: []admintable.Column{
			{Key: "name", Label: "Name", Sortable: true, SQLExpr: "p.name"},
		},
		DefaultKey: "name",
		DefaultDir: admintable.Asc,
	},
	Perms: resource.ReadAny,
	Lister: func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		const total = 120
		var rows []resource.Row
		for i := q.Offset; i < total && i < q.Offset+q.Limit; i++ {
			rows = append(rows, resource.Row{
				ID:    fmt.Sprintf("%d", i),
				Cells: []resource.Cell{{Value: fmt.Sprintf("row-%03d", i)}},
			})
		}
		return rows, total, nil
	},
}

// loginCookie performs the HMAC login dance and returns the session cookie value.
func loginCookie(t *testing.T) string {
	t.Helper()
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
		Secure:   false,
	})
	body := strings.NewReader("username=admin&password=secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, req)
	return extractCookieValue(w.Header().Get("Set-Cookie"), "panel_admin")
}

func getPage(t *testing.T, p *resource.Panel, cookie, path string, htmx bool) string {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookie})
	if htmx {
		r.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d\nbody: %s", path, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// TestListPage_FullPageContainsRegionTarget is the regression test for the
// broken first click: sort/filter/pagination htmx links target
// #{name}-region, so the FULL page must render that wrapper — before the fix
// only the /rows fragment did, and the first swap had no target.
func TestListPage_FullPageContainsRegionTarget(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, pagedResource)
	cookie := loginCookie(t)

	body := getPage(t, p, cookie, "/admin/paged", false)
	if !strings.Contains(body, `id="paged-region"`) {
		t.Error("full list page must contain the #paged-region swap target")
	}
	if !strings.Contains(body, `hx-target="#paged-region"`) {
		t.Error("pagination links must target #paged-region")
	}
	if !strings.Contains(body, "1 / 3") {
		t.Errorf("expected page counter '1 / 3'")
	}
}

// TestListPage_LoadMoreAppendMode verifies append=1 returns bare rows (no
// table wrapper) plus an OOB pagination replacement advancing the counter.
func TestListPage_LoadMoreAppendMode(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, pagedResource)
	cookie := loginCookie(t)

	// The Load-more control must exist and point at append mode.
	full := getPage(t, p, cookie, "/admin/paged", false)
	if !strings.Contains(full, "append=1") || !strings.Contains(full, `hx-swap="beforeend"`) {
		t.Fatal("full page must render the Load-more append control")
	}
	if !strings.Contains(full, `id="paged-pagination"`) {
		t.Fatal("pagination must render its stable-id wrapper (OOB target)")
	}

	frag := getPage(t, p, cookie, "/admin/paged/rows?page=2&append=1", true)
	if strings.Contains(frag, "<table") {
		t.Error("append fragment must not contain a <table> wrapper")
	}
	if !strings.Contains(frag, "row-050") {
		t.Error("append fragment must contain page-2 rows")
	}
	if !strings.Contains(frag, `hx-swap-oob="outerHTML"`) || !strings.Contains(frag, `id="paged-pagination"`) {
		t.Error("append fragment must carry the OOB pagination replacement")
	}
	if !strings.Contains(frag, "2 / 3") {
		t.Error("OOB pagination must advance the counter to '2 / 3'")
	}
	// The advanced Load-more link must request page 3 and never leak append
	// into the region-swap Prev/Next links.
	if !strings.Contains(frag, "append=1") {
		t.Error("OOB pagination must include the next Load-more link")
	}
	for _, line := range strings.Split(frag, "<a ") {
		if strings.Contains(line, `hx-target="#paged-region"`) && strings.Contains(line, "append=1") {
			t.Errorf("region-swap link must not carry append=1: %s", line)
		}
	}
}

// TestListPage_RowsFragmentKeepsRegionWrapper pins the /rows contract the
// region swap depends on (outerHTML replace needs the wrapper in the response).
func TestListPage_RowsFragmentKeepsRegionWrapper(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, pagedResource)
	cookie := loginCookie(t)

	frag := getPage(t, p, cookie, "/admin/paged/rows?page=2", true)
	if !strings.Contains(frag, `id="paged-region"`) {
		t.Error("/rows fragment must keep the #paged-region wrapper")
	}
	if !strings.Contains(frag, "2 / 3") {
		t.Errorf("expected page counter '2 / 3'")
	}
}
