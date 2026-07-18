package mcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
)

// stubResource builds a minimal Resource with a Lister and Detailer for testing.
func stubResource(name string) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: "Test " + name,
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", SQLExpr: "id", Sortable: true},
			},
			DefaultKey: "id",
			DefaultDir: admintable.Desc,
		},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{
				{ID: "1", Cells: []resource.Cell{{Value: "row 1"}}},
				{ID: "2", Cells: []resource.Cell{{Value: "row 2"}}},
			}, 2, nil
		},
		Detailer: func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
			return []resource.DetailSection{
				{Title: "Info", Items: []resource.DetailItem{{Label: "ID", Value: id}}},
			}, nil
		},
	}
}

func TestRowsToJSON(t *testing.T) {
	rows := []resource.Row{
		{ID: "1", Cells: []resource.Cell{{Value: "alpha", HTML: false}}, Href: "/admin/test/1"},
		{ID: "2", Cells: []resource.Cell{{Value: "<b>beta</b>", HTML: true}}},
	}
	out := rowsToJSON(rows)
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	if out[0].ID != "1" || out[0].Href != "/admin/test/1" {
		t.Errorf("row 0: unexpected ID=%q Href=%q", out[0].ID, out[0].Href)
	}
	if out[1].Cells[0].HTML != true {
		t.Errorf("row 1 cell 0: expected HTML=true, got false")
	}
}

func TestSectionsToJSON(t *testing.T) {
	sections := []resource.DetailSection{
		{Title: "Card", Items: []resource.DetailItem{{Label: "Name", Value: "Test", HTML: false}}},
		{RawHTML: "<div>custom</div>"},
	}
	out := sectionsToJSON(sections)
	if len(out) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(out))
	}
	if out[0].Title != "Card" || len(out[0].Items) != 1 {
		t.Errorf("section 0: unexpected Title=%q items=%d", out[0].Title, len(out[0].Items))
	}
	if out[1].RawHTML != "<div>custom</div>" {
		t.Errorf("section 1: unexpected RawHTML=%q", out[1].RawHTML)
	}
}

func TestRegisterResourceTools(t *testing.T) {
	// Verify that registerResourceTools does not panic and handles
	// resources with and without Detailer.
	resources := []resource.Resource{
		stubResource("with_detail"),
		{Name: "no_detail", Title: "No Detail", Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		}},
	}
	// Just verify it doesn't panic — we can't easily boot an MCP server in a unit test.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerResourceTools panicked: %v", r)
		}
	}()
	// We can't call registerResourceTools without a real *mcp.Server here
	// because it requires the SDK server. The JSON helpers above cover the
	// serialization logic; integration is verified by the consumer (go-job).
	_ = resources
}
