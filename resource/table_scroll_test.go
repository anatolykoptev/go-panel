package resource_test

// A resource with many columns renders a table wider than the viewport. The
// scroll container that keeps that width off the document has to live INSIDE
// the htmx swap target, because a sort/filter/pagination click replaces that
// target wholesale. A wrapper added only to the first-paint path looks correct
// on load and vanishes on the first click — the page starts scrolling
// sideways again with nothing in the logs and nothing red.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// Falsification: in resource/list.templ, move `<div class="crm-table-scroll">`
// out of listRowsFragment so it wraps the fragment call in listPageContent
// instead — the full page still carries it, the /rows fragment does not, and
// the second subtest goes RED.
func TestWideTableScrollsInsideItself(t *testing.T) {
	// 14 columns, the shape measured on go-grad /admin/leads.
	keys := []string{"received", "source", "name", "company", "contact",
		"telegram", "budget", "status", "sent", "page_title", "page_url",
		"page_section", "wanted", "comment"}
	cols := make([]admintable.Column, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, admintable.Column{
			Key: k, Label: strings.ToUpper(k), SQLExpr: k, Sortable: true,
		})
	}

	res := resource.Resource{
		Name: "leads", Title: "Leads", Icon: "*",
		Sort: admintable.Spec{Columns: cols, DefaultKey: "received", DefaultDir: admintable.Desc},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			cells := make([]resource.Cell, len(cols))
			for i := range cells {
				cells[i] = resource.Cell{Value: "x"}
			}
			return []resource.Row{{ID: "1", Cells: cells}}, 1, nil
		},
	}

	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw",
		HMACKey: []byte("0123456789abcdef0123456789abcdef"), BasePath: "/admin"})
	p := resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a})
	resource.Register(p, res)
	h := p.Handler()

	lrec := httptest.NewRecorder()
	lreq := httptest.NewRequest(http.MethodPost, "/admin/login",
		strings.NewReader("username=op&password=pw"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.LoginHandler().ServeHTTP(lrec, lreq)
	cookies := lrec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login produced no cookie (status %d)", lrec.Code)
	}

	// The full page and the fragment htmx swaps in are DIFFERENT render paths.
	// Both must carry the container, or the fix only survives until a click.
	for _, tc := range []struct{ name, path string }{
		{"full page", "/admin/leads"},
		{"htmx rows fragment", "/admin/leads/rows?sort=received&dir=asc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			for _, c := range cookies {
				req.AddCookie(c)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			body := rec.Body.String()

			// Match the ATTRIBUTE, not the bare class name: the stylesheet in
			// <head> mentions the name too, and matching that would make this
			// pass on a page carrying only the CSS rule and no container.
			const wrapper = `class="crm-table-scroll"`
			if n := strings.Count(body, wrapper); n != 1 {
				t.Fatalf("scroll container appears %d times, want exactly 1", n)
			}
			if !strings.Contains(body, `class="crm-table"`) {
				t.Fatal("no table rendered — the assertion above proved nothing")
			}
			region, wrap := strings.Index(body, `id="leads-region"`), strings.Index(body, wrapper)
			if region < 0 {
				t.Fatal(`no swap target ("leads-region") in this render path`)
			}
			if wrap < region {
				t.Error("container sits outside the swap target — the first sort " +
					"click will replace the target and take the container with it")
			}
		})
	}
}
