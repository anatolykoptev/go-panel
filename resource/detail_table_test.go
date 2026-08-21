package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
)

// detailTableResource builds a Resource whose Detailer returns the given sections.
func detailTableResource(name string, sections []resource.DetailSection) resource.Resource {
	r := detailerResource(name, func(_ context.Context, _ *http.Request, _ string) ([]resource.DetailSection, error) {
		return sections, nil
	})
	return r
}

// renderDetail fires a GET against a panel with one resource and returns the body.
func renderDetail(t *testing.T, r resource.Resource, id string) string {
	t.Helper()
	a := newTestAuth()
	p := resource.New(resource.Config{Title: "T", BasePath: "/admin", Auth: a})
	resource.Register(p, r)
	cookie := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/"+r.Name+"/"+id, nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookie})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// TestDetailTable_RendersStaticClass asserts the detail table carries the
// crm-table--static modifier (F1 guard: deleting the modifier goes RED).
func TestDetailTable_RendersStaticClass(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "Lines", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}, {Label: "B"}},
			Rows:    [][]resource.DetailCell{{{Value: "1"}, {Value: "2"}}},
		}},
	}), "1")
	if !strings.Contains(body, `class="crm-table crm-table--static"`) {
		t.Fatalf("detail table missing class=\"crm-table crm-table--static\"")
	}
}

// TestDetailTable_ItemsBeforeTable asserts Items render ABOVE the Table (F2 guard).
func TestDetailTable_ItemsBeforeTable(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Items: []resource.DetailItem{{Label: "L", Value: "V"}}, Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}},
			Rows:    [][]resource.DetailCell{{{Value: "1"}}},
		}},
	}), "1")
	itemsIdx := strings.Index(body, `class="detail-items"`)
	tableIdx := strings.Index(body, "<table")
	if itemsIdx < 0 || tableIdx < 0 {
		t.Fatalf("expected both detail-items and <table in body")
	}
	if itemsIdx >= tableIdx {
		t.Fatalf("detail-items (idx %d) must appear before <table (idx %d)", itemsIdx, tableIdx)
	}
}

// TestDetailTable_RawHTMLSuppressesTable asserts a section with BOTH RawHTML and
// Table set renders RawHTML and NO table (F5 guard).
func TestDetailTable_RawHTMLSuppressesTable(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", RawHTML: `<div class="fit-card">x</div>`, Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}},
			Rows:    [][]resource.DetailCell{{{Value: "1"}}},
		}},
	}), "1")
	if !strings.Contains(body, `class="fit-card"`) {
		t.Fatalf("RawHTML block not rendered")
	}
	if strings.Contains(body, "<table") {
		t.Fatalf("section with RawHTML set must NOT also render a table")
	}
}

// TestDetailTable_CellEscaped asserts cell Value is HTML-escaped, never raw
// (F4 guard: @templ.Raw(cell.Value) would emit raw <b>).
func TestDetailTable_CellEscaped(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}},
			Rows:    [][]resource.DetailCell{{{Value: "<b>x</b>"}}},
		}},
	}), "1")
	if strings.Contains(body, "<b>x</b>") {
		t.Fatalf("cell value rendered raw (XSS): %q", body)
	}
	if !strings.Contains(body, "&lt;b&gt;") {
		t.Fatalf("cell value not HTML-escaped as expected")
	}
}

// TestDetailTable_HrefAnchor asserts a cell with Href renders an anchor and the
// Value stays escaped text inside it.
func TestDetailTable_HrefAnchor(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}},
			Rows:    [][]resource.DetailCell{{{Value: "go", Href: "/admin/leads?x=1"}}},
		}},
	}), "1")
	if !strings.Contains(body, `href="/admin/leads?x=1"`) {
		t.Fatalf("cell href not rendered: %q", body)
	}
	if !strings.Contains(body, ">go</a>") {
		t.Fatalf("cell anchor text not rendered")
	}
}

// TestDetailTable_AlignEmitted asserts Align renders text-align on th AND td, and
// an empty Align emits no style attribute.
func TestDetailTable_AlignEmitted(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}, {Label: "B", Align: "right"}},
			Rows:    [][]resource.DetailCell{{{Value: "1"}, {Value: "2"}}},
		}},
	}), "1")
	if !strings.Contains(body, `text-align:right`) {
		t.Fatalf("right-align style not emitted")
	}
	// Count text-align occurrences: one on th, one on td = 2.
	if got := strings.Count(body, "text-align:right"); got != 2 {
		t.Fatalf("expected text-align:right on th+td (2), got %d", got)
	}
}

// TestDetailTable_ShortRowPadded asserts a row with fewer cells than columns
// renders empty <td> for the remainder (no index panic, no missing cells).
func TestDetailTable_ShortRowPadded(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}, {Label: "B"}, {Label: "C"}},
			Rows:    [][]resource.DetailCell{{{Value: "only-one"}}},
		}},
	}), "1")
	// 3 columns => 3 <td> in the row.
	if got := strings.Count(body, "<td"); got < 3 {
		t.Fatalf("short row should yield 3 <td> (padded), got %d <td occurrences", got)
	}
}

// TestDetailTable_LongRowTruncated asserts a row with MORE cells than columns
// renders only the first len(Columns) cells.
func TestDetailTable_LongRowTruncated(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "A"}},
			Rows:    [][]resource.DetailCell{{{Value: "keep-this"}, {Value: "TRUNCATED_EXTRA"}}},
		}},
	}), "1")
	if strings.Contains(body, "TRUNCATED_EXTRA") {
		t.Fatalf("extra cell beyond columns must not render, found 'TRUNCATED_EXTRA'")
	}
	if !strings.Contains(body, "keep-this") {
		t.Fatalf("first cell must render, missing 'keep-this'")
	}
}

// TestDetailTable_NoColumnsNoTable asserts len(Columns)==0 renders nothing.
func TestDetailTable_NoColumnsNoTable(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "S", Table: resource.DetailTable{
			Columns: nil,
			Rows:    [][]resource.DetailCell{{{Value: "x"}}},
		}},
	}), "1")
	if strings.Contains(body, "<table") {
		t.Fatalf("table with zero columns must not render")
	}
}

// --- FilterLinkCell ---

// TestFilterLinkCell_RendersAnchor asserts the basic href shape.
func TestFilterLinkCell_RendersAnchor(t *testing.T) {
	got := resource.FilterLinkCell("/admin", "leads", "merchant_id", "42", "Acme")
	want := `<a href="/admin/leads?merchant_id=42">Acme</a>`
	if got != want {
		t.Fatalf("FilterLinkCell = %q, want %q", got, want)
	}
}

// TestFilterLinkCell_XSSValueEscaped (F3 guard): a filter value carrying a
// script payload must NOT produce a raw <script> in the output.
func TestFilterLinkCell_XSSValueEscaped(t *testing.T) {
	got := resource.FilterLinkCell("/admin", "leads", "q", `1"><script>alert(1)</script>`, "x")
	if strings.Contains(got, "<script") {
		t.Fatalf("filter value injected raw <script> (stored XSS): %q", got)
	}
	if strings.Contains(got, "%3Cscript") || strings.Contains(got, "%3C%2Fscript") {
		// url.QueryEscape neutralizes it in the query — acceptable.
		return
	}
	t.Fatalf("filter value not neutralized via url.QueryEscape: %q", got)
}

// TestFilterLinkCell_XSSLabelEscaped asserts the label is HTML-escaped for text.
func TestFilterLinkCell_XSSLabelEscaped(t *testing.T) {
	got := resource.FilterLinkCell("/admin", "leads", "q", "1", "<script>alert(1)</script>")
	if strings.Contains(got, "<script>") {
		t.Fatalf("label injected raw <script>: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("label not HTML-escaped: %q", got)
	}
}

// TestFilterLinkCell_QueryEscaped asserts the query key and value are
// url.QueryEscape'd (spaces, &, etc.), not concatenated raw.
func TestFilterLinkCell_QueryEscaped(t *testing.T) {
	got := resource.FilterLinkCell("/admin", "leads", "merchant name", "a&b", "x")
	if !strings.Contains(got, "merchant+name") && !strings.Contains(got, "merchant%20name") {
		t.Fatalf("query key not url.QueryEscape'd: %q", got)
	}
	if !strings.Contains(got, "a%26b") {
		t.Fatalf("query value '&' not url.QueryEscape'd to %%26: %q", got)
	}
}

// TestFilterLinkCell_ResourcePathEscaped asserts resourceName is URL-path-escaped.
func TestFilterLinkCell_ResourcePathEscaped(t *testing.T) {
	got := resource.FilterLinkCell("/admin", "billable leads", "q", "1", "x")
	if strings.Contains(got, "/billable leads?") {
		t.Fatalf("resourceName with space not path-escaped: %q", got)
	}
}

// F6 — a dangerous scheme in DetailCell.Href must not survive into the anchor.
//
// templ.SafeURL is NOT a sanitiser; it is the opt-OUT from templ's URL
// sanitiser, and casting an arbitrary string to it asserts the string is
// already safe. A cell Href is consumer data built from a database row, so that
// assertion is false by construction. Passing the plain string lets templ
// rewrite the scheme instead.
//
// This matters more here than almost anywhere else in the package: DetailTable
// exists so consumers stop hand-writing anchors through the RawHTML hatch, and
// a primitive that escapes worse than the code it replaces is a net loss.
//
// RED-on-revert: in detail.templ, wrap the href back in templ.SafeURL(...)
// -> "javascript:" reaches the rendered anchor verbatim. Measured.
func TestDetailTable_HrefSchemeSanitized(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Title: "Invoices", Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "Period"}},
			Rows: [][]resource.DetailCell{
				{{Value: "2026-08", Href: "javascript:alert(1)"}},
			},
		}},
	}), "1")

	if strings.Contains(body, "javascript:") {
		t.Errorf("a javascript: href survived into the anchor:\n%s", body)
	}
	if !strings.Contains(body, "TemplFailedSanitizationURL") {
		t.Errorf("want templ's sanitiser to have rewritten the href, got:\n%s", body)
	}
}

// A legitimate href must survive the sanitiser unchanged — otherwise F6 could
// be satisfied by a primitive that silently drops every link.
func TestDetailTable_LegitimateHrefSurvives(t *testing.T) {
	body := renderDetail(t, detailTableResource("things", []resource.DetailSection{
		{Table: resource.DetailTable{
			Columns: []resource.DetailColumn{{Label: "Period"}},
			Rows: [][]resource.DetailCell{
				{{Value: "2026-08", Href: "/admin/invoices?account_id=7"}},
			},
		}},
	}), "1")

	if !strings.Contains(body, `href="/admin/invoices?account_id=7"`) {
		t.Errorf("a legitimate href did not survive:\n%s", body)
	}
}
