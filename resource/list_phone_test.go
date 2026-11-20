package resource_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
)

func phoneResource(allowed []string, rows []resource.Row) resource.Resource {
	return resource.Resource{
		Name: "leads", Title: "Leads",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Label: "Name", SQLExpr: "name", Sortable: true},
				{Key: "value", Label: "Value", SQLExpr: "value", Align: "right"},
				{Key: "group", Label: "Group", SQLExpr: "grp"},
				{Key: "rev", Label: "Rev", SQLExpr: "rev", Align: "right"},
			},
			DefaultKey: "name", DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "group", SQLExpr: "grp", Match: admintable.Eq, Allowed: allowed},
		}},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return rows, len(rows), nil
		},
	}
}

func opts(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i))
	}
	return out
}

// More than six options is a <select>; six or fewer stay chips. The select
// carries an "All" option that submits no filter, and marks the active value.
//
// RED-on-revert: change `> maxFilterChips` in filterAsSelect (resource/list.templ)
// to `> 99` -> the seven-option case renders chips and this fails.
func TestFilterBar_SelectPastSixOptions(t *testing.T) {
	seven := renderList(t, phoneResource(opts(7), nil), "?group=c")
	if !strings.Contains(seven, `<select id="filter-group" name="group" class="filter-select">`) {
		t.Fatalf("seven options must render a select:\n%s", chipSnippet(seven))
	}
	if strings.Contains(seven, `name="group" value="a"`) {
		t.Errorf("seven options must not also render chips")
	}
	if !strings.Contains(seven, `<option value="">All</option>`) {
		t.Errorf("select lacks the All option")
	}
	if !strings.Contains(seven, `<option value="c" selected>c</option>`) {
		t.Errorf("active value not selected")
	}
	if strings.Count(seven, `" selected>`) != 1 {
		t.Errorf("exactly one option may be selected")
	}

	six := renderList(t, phoneResource(opts(6), nil), "")
	if strings.Contains(six, "<select") || !strings.Contains(six, `name="group" value="f"`) {
		t.Errorf("six options must stay chips:\n%s", chipSnippet(six))
	}
}

// An empty list says why: filtered -> sentence + a Clear filters link that
// drops the filter but keeps the sort; unfiltered -> plain sentence, no action.
//
// RED-on-revert: make filtersActive (resource/list.templ) return false -> the
// filtered case loses its link.
func TestEmptyState_FilteredOffersClear(t *testing.T) {
	body := renderList(t, phoneResource(opts(3), nil), "?group=a&sort=name&dir=desc&page=2")
	if !strings.Contains(body, "No results match these filters.") {
		t.Fatalf("filtered empty state sentence missing")
	}
	m := regexp.MustCompile(`<a class="btn btn-secondary" href="([^"]*)">Clear filters</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("Clear filters link missing")
	}
	href := strings.ReplaceAll(m[1], "&amp;", "&")
	if strings.Contains(href, "group=") || strings.Contains(href, "page=") || !strings.Contains(href, "sort=name") || !strings.HasPrefix(href, "/admin/leads") {
		t.Errorf("clear href = %q: must drop group and page, keep sort", href)
	}

	plain := renderList(t, phoneResource(opts(3), nil), "")
	if strings.Contains(plain, "Clear filters") || !strings.Contains(plain, "No results") {
		t.Errorf("unfiltered empty state must not offer Clear filters")
	}
}

// Phone card roles: first cell title, first right-aligned cell after it value,
// the rest meta; CardMeta hides the others (phones only, via cell-hide).
//
// RED-on-revert: change `i == cardValueIdx(cols)` in cellClass to `false` -> the
// value cell becomes meta.
func TestRowCells_CardRoles(t *testing.T) {
	rows := []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "a"}, {Value: "9"}, {Value: "g"}, {Value: "3"}}}}
	body := renderList(t, phoneResource(opts(2), rows), "")
	for _, want := range []string{`<td class="cell-title"`, `<td class="cell-value"`, `<td class="cell-meta"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(body, `class="cell-meta"`) != 2 { // group + rev
		t.Errorf("want two meta cells, got %d", strings.Count(body, `class="cell-meta"`))
	}

	res := phoneResource(opts(2), rows)
	res.CardMeta = []string{"rev"}
	body = renderList(t, res, "")
	if strings.Count(body, `class="cell-meta cell-hide"`) != 1 || strings.Count(body, `class="cell-meta"`) != 1 {
		t.Errorf("CardMeta=[rev] must hide exactly the group cell:\n%s", body)
	}
}

func TestCardMeta_UnknownKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Register must panic on a CardMeta key that is not a sort column")
		}
	}()
	res := phoneResource(opts(2), nil)
	res.CardMeta = []string{"nope"}
	renderList(t, res, "")
}
