package resource_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
)

func TestResourceHeader_NotResolvedForFragments(t *testing.T) {
	calls := 0
	p := newWriterPanel()
	r := minimalResource("ledger")
	r.Lister = func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) { return nil, 0, nil }
	r.Header = func(_ context.Context, _ *http.Request) templ.Component {
		calls++
		return templ.Raw("<p>h</p>")
	}
	resource.Register(p, r)
	cookie := loginCookie(t)
	getPage(t, p, cookie, "/admin/ledger/rows", true)
	getPage(t, p, cookie, "/admin/ledger", true)
	if calls != 0 {
		t.Fatalf("Header ran %d times for htmx fragment requests, want 0", calls)
	}
	getPage(t, p, cookie, "/admin/ledger", false)
	if calls != 1 {
		t.Fatalf("Header ran %d times for one full-page render, want 1", calls)
	}
}

func TestResourceHeader_RenderedBetweenTitleAndTable(t *testing.T) {
	for _, withHeader := range []bool{true, false} {
		p := newWriterPanel()
		r := minimalResource("ledger")
		r.Filter = admintable.FilterSpec{Filters: []admintable.Filter{{Key: "q", SQLExprs: []string{"id"}, Match: admintable.ILike}}}
		r.Lister = func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) { return nil, 0, nil }
		if withHeader {
			r.Header = func(_ context.Context, _ *http.Request) templ.Component {
				return templ.Raw(`<p id="legend">pending rows are tinted</p>`)
			}
		}
		resource.Register(p, r)
		cookie := loginCookie(t)

		page := getPage(t, p, cookie, "/admin/ledger", false)
		has := strings.Contains(page, `id="legend"`)
		if has != withHeader {
			t.Fatalf("withHeader=%v but legend rendered=%v", withHeader, has)
		}
		if withHeader {
			iTitle := strings.Index(page, `class="page-header"`)
			iLegend := strings.Index(page, `id="legend"`)
			iTable := strings.Index(page, `id="ledger-region"`)
			if iTitle < 0 || iTitle >= iLegend || iLegend >= iTable {
				t.Fatalf("legend not between page header and table region: title@%d legend@%d table@%d", iTitle, iLegend, iTable)
			}
			if frag := getPage(t, p, cookie, "/admin/ledger/rows", true); strings.Contains(frag, `id="legend"`) {
				t.Fatal("htmx rows fragment re-rendered the header")
			}
		}
	}
}
