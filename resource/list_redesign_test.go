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

func statusResource(rows []resource.Row, labels map[string]map[string]string) resource.Resource {
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
			{Key: "status", SQLExpr: "status", Match: admintable.Eq, Allowed: []string{"new", "won"}},
		}},
		FilterLabels: labels,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return rows, len(rows), nil
		},
	}
}

func renderList(t *testing.T, res resource.Resource, query string) string {
	t.Helper()
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw",
		HMACKey: []byte("0123456789abcdef0123456789abcdef"), BasePath: "/admin"})
	p := resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a})
	resource.Register(p, res)
	h := p.Handler()
	lreq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login",
		strings.NewReader("username=op&password=pw"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lrec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(lrec, lreq)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/leads"+query, nil)
	for _, c := range lrec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestFilterLabels_ChipShowsLabelSubmitsValue: the chip text is the label, the
// submitted value is unchanged, an unlabelled value falls back to itself, and
// the active class still keys on the VALUE.
//
// Falsification: in list.templ render `{ a }` instead of
// `{ filterChipLabel(...) }` -> RED (label absent).
func TestFilterLabels_ChipShowsLabelSubmitsValue(t *testing.T) {
	body := renderList(t, statusResource(nil, map[string]map[string]string{"status": {"new": "Новая"}}), "?status=new")
	if !regexp.MustCompile(`name="status"\s+value="new"[^>]*class="filter-chip active"[^>]*>Новая</button>`).MatchString(body) {
		t.Errorf("labelled chip missing / not active / value changed:\n%s", chipSnippet(body))
	}
	if !regexp.MustCompile(`value="won"[^>]*>won</button>`).MatchString(body) {
		t.Errorf("unlabelled chip must fall back to its value:\n%s", chipSnippet(body))
	}
}

func TestFilterLabels_AbsentIsBackwardCompatible(t *testing.T) {
	body := renderList(t, statusResource(nil, nil), "")
	if !regexp.MustCompile(`value="new"[^>]*>new</button>`).MatchString(body) {
		t.Errorf("no FilterLabels: chip must show value:\n%s", chipSnippet(body))
	}
}

func chipSnippet(body string) string {
	i := strings.Index(body, `class="filter-bar"`)
	if i < 0 {
		return body
	}
	return body[i:min(len(body), i+900)]
}

func TestFilterLabels_RegisterPanicsOnTypo(t *testing.T) {
	for name, labels := range map[string]map[string]map[string]string{
		"unknown key":   {"nope": {"new": "X"}},
		"unknown value": {"status": {"zzz": "X"}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register accepted a FilterLabels typo")
				}
			}()
			a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw",
				HMACKey: []byte("0123456789abcdef0123456789abcdef"), BasePath: "/admin"})
			resource.Register(resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a}), statusResource(nil, labels))
		})
	}
}

// Falsification: drop `templ.KV("row-emphasis", row.Emphasis)` from listRow -> RED.
func TestRowEmphasis_OnlyEmphasisedRowsGetClass(t *testing.T) {
	rows := []resource.Row{
		{ID: "1", Emphasis: true, Cells: []resource.Cell{{Value: "Unread"}, {Value: "new"}}},
		{ID: "2", Cells: []resource.Cell{{Value: "Read"}, {Value: "won"}}},
	}
	body := renderList(t, statusResource(rows, nil), "")
	if n := strings.Count(body, `<tr class="row-emphasis">`); n != 1 {
		t.Errorf("want exactly 1 emphasised row, got %d", n)
	}
}

// The stacked-card CSS prints data-label before each value; without the
// attribute a phone user sees bare values with no idea which column is which.
// Falsification: drop data-label from the plain-text <td> in listRow -> RED.
func TestListCells_CarryColumnLabelForStackedMode(t *testing.T) {
	rows := []resource.Row{{ID: "1", Href: "/admin/leads/1", Cells: []resource.Cell{{Value: "Anna"}, {Value: "<b>x</b>", HTML: true}}}}
	body := renderList(t, statusResource(rows, nil), "")
	for _, want := range []string{`data-label="Name"`, `data-label="Status"`, `<table class="crm-table" data-stack="cards"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}
