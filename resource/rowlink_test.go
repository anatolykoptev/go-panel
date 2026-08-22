package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
)

// The panel is mounted somewhere OTHER than /admin in every test here. At the
// default a href built from a hardcoded "/admin" is indistinguishable from one
// built from d.BasePath, so the test that matters cannot fail.
const rowLinkMount = "/operator-panel"

var rowNameHrefRe = regexp.MustCompile(`<a class="row-name" href="([^"]*)"`)

// listRowsResource is a resource whose Lister returns exactly the rows given,
// so each test controls Row.ID and Row.Href directly.
func listRowsResource(name string, rows []resource.Row) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: name,
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return rows, len(rows), nil
		},
	}
}

func getAsAdmin(t *testing.T, p *resource.Panel, a interface {
	Verify(string) (string, bool)
}, path string,
) *httptest.ResponseRecorder {
	t.Helper()
	_ = a
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: rowLinkCookie(t)})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// rowLinkAuth/rowLinkCookie keep one auth instance for the whole file so the
// cookie minted here verifies against the panel each test builds.
var rowLinkAuthInstance = newTestAuth()

func rowLinkCookie(t *testing.T) string {
	t.Helper()
	return authCookie(t, rowLinkAuthInstance, "admin", "secret")
}

func mountRowLink(t *testing.T, r resource.Resource) *resource.Panel {
	t.Helper()
	p := resource.New(resource.Config{Title: "T", BasePath: rowLinkMount, Auth: rowLinkAuthInstance})
	resource.Register(p, r)
	return p
}

func firstCellHref(t *testing.T, body string) string {
	t.Helper()
	m := rowNameHrefRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// A resource with a Detailer links its first cell at the detail route, and that
// route answers. Asserting the string alone would only prove the template
// agrees with the test; fetching it proves the link and the mount agree.
//
// RED-on-revert: drop the EffectiveDetailer branch from rowDetailHref and the
// href goes empty, so both the equality and the follow-up GET fail.
func TestRowLink_DetailerResourceLinksToARouteThatAnswers(t *testing.T) {
	r := listRowsResource("things", []resource.Row{{ID: "42", Cells: []resource.Cell{{Value: "first"}}}})
	r.Detailer = func(context.Context, *http.Request, string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "T", Items: []resource.DetailItem{{Label: "L", Value: "V"}}}}, nil
	}
	p := mountRowLink(t, r)

	w := getAsAdmin(t, p, nil, rowLinkMount+"/things")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d\n%s", w.Code, w.Body.String())
	}
	href := firstCellHref(t, w.Body.String())
	if want := rowLinkMount + "/things/42"; href != want {
		t.Fatalf("first-cell href = %q, want %q — the name column is the only way an "+
			"operator reaches a client page; without it the sole row link is Edit, "+
			"which lands on the settings form", href, want)
	}

	got := getAsAdmin(t, p, nil, href)
	if got.Code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200 — the link points somewhere the panel does not serve", href, got.Code)
	}
}

// A resource that sets ONLY FetchRow has a detail route: Register synthesizes
// the Detailer into a COPY and mounts that, leaving Resource.Detailer nil. A
// predicate reading the field directly leaves exactly these resources unlinked,
// and the symptom is silent — a name that simply is not a link.
//
// RED-on-revert: change rowDetailHref to test `d.Resource.Detailer != nil`.
func TestRowLink_FetchRowOnlyResourceIsAlsoLinked(t *testing.T) {
	r := listRowsResource("gadgets", []resource.Row{{ID: "7", Cells: []resource.Cell{{Value: "first"}}}})
	r.FetchRow = func(context.Context, string) (map[string]string, error) {
		return map[string]string{"id": "7"}, nil
	}
	p := mountRowLink(t, r)

	w := getAsAdmin(t, p, nil, rowLinkMount+"/gadgets")
	href := firstCellHref(t, w.Body.String())
	if want := rowLinkMount + "/gadgets/7"; href != want {
		t.Fatalf("FetchRow-only resource: href = %q, want %q", href, want)
	}
	if got := getAsAdmin(t, p, nil, href); got.Code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", href, got.Code)
	}
}

// No Detailer and no FetchRow means no detail page, so the name must stay plain
// text. A link to a 404 is worse than no link: it reads as a feature.
func TestRowLink_ResourceWithoutADetailPageRendersPlainText(t *testing.T) {
	r := listRowsResource("flat", []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "first"}}}})
	p := mountRowLink(t, r)

	w := getAsAdmin(t, p, nil, rowLinkMount+"/flat")
	if href := firstCellHref(t, w.Body.String()); href != "" {
		t.Errorf("resource with no detail page linked its first cell to %q", href)
	}
}

// Row.Href remains the override, for a detail page owned by a DIFFERENT
// resource. The derived path must not win over an explicit one.
func TestRowLink_ExplicitRowHrefWinsOverTheDerivedPath(t *testing.T) {
	const elsewhere = rowLinkMount + "/other-resource/99"
	r := listRowsResource("things", []resource.Row{{ID: "42", Href: elsewhere, Cells: []resource.Cell{{Value: "first"}}}})
	r.Detailer = func(context.Context, *http.Request, string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "T"}}, nil
	}
	p := mountRowLink(t, r)

	w := getAsAdmin(t, p, nil, rowLinkMount+"/things")
	if href := firstCellHref(t, w.Body.String()); href != elsewhere {
		t.Errorf("href = %q, want the explicit %q", href, elsewhere)
	}
}

// templ.SafeURL is the opt-OUT of templ's URL sanitiser, so casting a
// consumer-supplied Row.Href to it renders javascript: verbatim in the href.
// This is the same defect already fixed once in detail.templ; list.templ kept
// it until now.
//
// RED-on-revert: wrap the href back in templ.SafeURL.
func TestRowLink_DangerousSchemeIsNeutralised(t *testing.T) {
	r := listRowsResource("things", []resource.Row{{ID: "1", Href: "javascript:alert(1)", Cells: []resource.Cell{{Value: "first"}}}})
	p := mountRowLink(t, r)

	body := getAsAdmin(t, p, nil, rowLinkMount+"/things").Body.String()
	if got := firstCellHref(t, body); got != "about:invalid#TemplFailedSanitizationURL" {
		t.Errorf("javascript: href rendered as %q — templ did not sanitise it", got)
	}
}

// The paired legitimate-input test. Without it the sanitisation test above is
// satisfied by a renderer that drops every link, and "fix by deletion" ships.
func TestRowLink_LegitimateHrefSurvives(t *testing.T) {
	const ok = "/operator-panel/things/1"
	r := listRowsResource("things", []resource.Row{{ID: "1", Href: ok, Cells: []resource.Cell{{Value: "first"}}}})
	p := mountRowLink(t, r)

	body := getAsAdmin(t, p, nil, rowLinkMount+"/things").Body.String()
	if got := firstCellHref(t, body); got != ok {
		t.Errorf("legitimate href = %q, want %q", got, ok)
	}
}

// An ID is a database value. Concatenated raw, one containing a slash builds a
// URL pointing at a different resource entirely, with no error anywhere.
func TestRowLink_IDIsPathEscaped(t *testing.T) {
	r := listRowsResource("things", []resource.Row{{ID: "a/b", Cells: []resource.Cell{{Value: "first"}}}})
	r.Detailer = func(context.Context, *http.Request, string) ([]resource.DetailSection, error) {
		return []resource.DetailSection{{Title: "T"}}, nil
	}
	p := mountRowLink(t, r)

	body := getAsAdmin(t, p, nil, rowLinkMount+"/things").Body.String()
	if want := rowLinkMount + "/things/a%2Fb"; firstCellHref(t, body) != want {
		t.Errorf("href = %q, want %q — a raw slash silently repoints the link",
			firstCellHref(t, body), want)
	}
}

// A cross-link built by ResolveTargets must point at a route the panel serves.
// Asserting the string alone only proves the resolver agrees with the test;
// fetching it proves the resolver agrees with the MOUNT — which is the half
// that was wrong when a merchant_id was used as a client's id.
func TestRelationTarget_CrossLinkResolvesToARouteThatAnswers(t *testing.T) {
	const orgID = "7e55f17f-89e8-470b-a5a8-ad5231b26efa"

	clients := listRowsResource("clients", []resource.Row{{ID: orgID, Cells: []resource.Cell{{Value: "Acme"}}}})
	clients.Detailer = func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
		if id != orgID {
			return nil, resource.ErrDetailNotFound
		}
		return []resource.DetailSection{{Title: "Org", Items: []resource.DetailItem{{Label: "Name", Value: "Acme"}}}}, nil
	}

	leads := listRowsResource("leads", []resource.Row{{ID: "5", Cells: []resource.Cell{{Value: "5"}, {Value: "1"}}}})
	leads.Sort.Columns = append(leads.Sort.Columns, admintable.Column{Key: "merchant_id", Sortable: true, SQLExpr: "l.merchant_id"})
	leads.Relations = []resource.Relation{{
		Resource:   "clients",
		ForeignKey: "merchant_id",
		DisplayKey: "name",
		ResolveTargets: func(_ context.Context, ids []string) (map[string]resource.RelationTarget, error) {
			out := map[string]resource.RelationTarget{}
			for _, id := range ids {
				if id == "1" {
					out[id] = resource.RelationTarget{ID: orgID, Label: "Acme"}
				}
			}
			return out, nil
		},
	}}

	p := resource.New(resource.Config{Title: "T", BasePath: rowLinkMount, Auth: rowLinkAuthInstance})
	resource.Register(p, clients)
	resource.Register(p, leads)

	w := getAsAdmin(t, p, nil, rowLinkMount+"/leads")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	m := regexp.MustCompile(`href="([^"]*/clients/[^"]*)"`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("no cross-link to clients rendered:\n%s", w.Body.String())
	}
	href := m[1]
	if want := rowLinkMount + "/clients/" + orgID; href != want {
		t.Fatalf("cross-link href = %q, want %q", href, want)
	}
	if got := getAsAdmin(t, p, nil, href); got.Code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200 — the cross-link points somewhere the panel does not serve", href, got.Code)
	}
}
