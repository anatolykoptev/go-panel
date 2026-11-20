package resource_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
)

func groupedRows(groups ...string) []resource.Row {
	rows := make([]resource.Row, len(groups))
	for i, g := range groups {
		rows[i] = resource.Row{ID: string(rune('a' + i)), Group: g,
			Cells: []resource.Cell{{Value: "n"}, {Value: "1"}, {Value: "g"}, {Value: "2"}}}
	}
	return rows
}

func headers(body string) []string {
	var out []string
	for _, part := range strings.Split(body, `<tr class="row-group">`)[1:] {
		a := strings.Index(part, `scope="colgroup">`) + len(`scope="colgroup">`)
		out = append(out, part[a:strings.Index(part, "</th>")])
	}
	return out
}

// A header row is drawn where Group changes, never for "", and a group that
// comes back gets a second header (the Lister owns the order).
//
// RED-on-revert: drop the `d.Rows[i-1].Group != row.Group` comparison in
// listRows (resource/list.templ) so a header is drawn for every row.
func TestRowGroup_HeaderWhereGroupChanges(t *testing.T) {
	body := renderList(t, phoneResource(opts(2), groupedRows("A", "A", "B", "", "A")), "")
	got := headers(body)
	want := []string{"A", "B", "A"}
	if len(got) != len(want) {
		t.Fatalf("headers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("headers = %v, want %v", got, want)
		}
	}
}

// No Group anywhere: no header row, markup as before.
func TestRowGroup_UngroupedListHasNone(t *testing.T) {
	if body := renderList(t, phoneResource(opts(2), groupedRows("", "")), ""); strings.Contains(body, `<tr class="row-group">`) {
		t.Fatal("ungrouped list must not draw a group header")
	}
}

// The name is plain text, escaped.
//
// RED-on-revert: render the name through templ.Raw.
func TestRowGroup_NameEscaped(t *testing.T) {
	body := renderList(t, phoneResource(opts(2), groupedRows("<b>x</b>")), "")
	if strings.Contains(body, "<b>x</b>") || !strings.Contains(body, "&lt;b&gt;x&lt;/b&gt;") {
		t.Fatal("group name must be escaped")
	}
}
