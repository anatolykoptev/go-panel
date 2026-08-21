// views_internal_test.go — falsification for Resource.Views (the mode chips a
// Lister receives as ListQuery.View) and for the selected-chip highlight.
//
// Both exist because the framework had a hole a consumer fell through: an
// aggregate resource whose GROUP BY grain the operator picks could not be
// expressed, so the page was hand-built downstream and lost the table with it.
package resource

import (
	"bytes"
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
)

// TestResolveView_UnknownFallsBackToFirst — ListQuery.View promises the Lister
// that the value is ALWAYS one of the declared keys, so a Lister may switch on
// it with no default branch. If a raw URL value could reach it, every Lister in
// every consumer would need its own validation and one of them would forget.
//
// Falsification: in resolveView, `return raw` instead of `return r.Views[0].Key`
// → RED.
func TestResolveView_UnknownFallsBackToFirst(t *testing.T) {
	r := Resource{Views: []View{{Key: "month"}, {Key: "week"}, {Key: "quarter"}}}
	for _, raw := range []string{"", "day", "MONTH", "month'); DROP TABLE x --", "../.."} {
		if got := r.resolveView(raw); got != "month" {
			t.Errorf("resolveView(%q) = %q, want the first declared view %q", raw, got, "month")
		}
	}
	for _, ok := range []string{"month", "week", "quarter"} {
		if got := r.resolveView(ok); got != ok {
			t.Errorf("resolveView(%q) = %q, want it unchanged", ok, got)
		}
	}
	var none Resource
	if got := none.resolveView("week"); got != "" {
		t.Errorf("a resource with no Views resolved %q, want the empty string", got)
	}
}

// TestValidateViewsConfig_RejectsEmptyAndDuplicateKeys — an empty key renders a
// chip that resolves to the default, so it looks selectable and does nothing; a
// duplicate makes the second chip unreachable. Neither errors at runtime and
// both are silently wrong on screen, which is what Register's startup panics are
// for.
//
// Falsification: delete the validateViewsConfig call in Register → RED.
func TestValidateViewsConfig_RejectsEmptyAndDuplicateKeys(t *testing.T) {
	for name, views := range map[string][]View{
		"empty key":     {{Key: "month"}, {Key: ""}},
		"duplicate key": {{Key: "month"}, {Key: "month", Label: "Monthly"}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Register accepted %s — a chip that cannot work shipped instead of failing the boot", name)
				}
			}()
			validateViewsConfig(Resource{Name: "revenue", Views: views})
		})
	}
	// A well-formed set must NOT panic, or the guard is just a wall.
	validateViewsConfig(Resource{Name: "revenue", Views: []View{{Key: "week"}, {Key: "month"}}})
}

// chipClassOf extracts the class attribute of the <button name=… value=…> chip,
// so an assertion names the chip it means instead of counting substrings across
// the whole document.
func chipClassOf(t *testing.T, body, name, value string) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<button[^>]*?name="` + regexp.QuoteMeta(name) +
		`"[^>]*?value="` + regexp.QuoteMeta(value) + `"[^>]*?class="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no chip %s=%s in:\n%s", name, value, body)
	}
	return m[1]
}

func renderFilterBar(t *testing.T, d listPageData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := filterBar(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render filter bar: %v", err)
	}
	return buf.String()
}

// TestFilterBar_MarksTheSelectedChipActive is the one that would have caught the
// original defect. `.filter-chip.active` has been in styles.templ since the chips
// themselves, and nothing ever applied it, so no admin list has ever shown which
// filter is on — dead CSS beside a missing affordance, and neither shows up as
// an error.
//
// Falsification: in chipClass, always `return "filter-chip"` → RED.
func TestFilterBar_MarksTheSelectedChipActive(t *testing.T) {
	d := listPageData{
		Resource: Resource{
			Name: "leads",
			Filter: admintable.FilterSpec{Filters: []admintable.Filter{
				{Key: "status", SQLExpr: "status", Match: admintable.AnyOf, Allowed: []string{"new", "done"}},
			}},
		},
		BasePath: "/admin",
		Selected: url.Values{"status": {"done"}},
	}
	body := renderFilterBar(t, d)
	if got := chipClassOf(t, body, "status", "done"); got != "filter-chip active" {
		t.Errorf(`the selected chip status=done has class %q, want "filter-chip active"`, got)
	}
	if got := chipClassOf(t, body, "status", "new"); got != "filter-chip" {
		t.Errorf(`the unselected chip status=new has class %q, want "filter-chip"`, got)
	}
	// Nothing selected → nothing active.
	d.Selected = url.Values{}
	if plain := renderFilterBar(t, d); strings.Contains(plain, "filter-chip active") {
		t.Errorf("a chip rendered active with no selection:\n%s", plain)
	}
}

// TestFilterBar_RendersViewChipsAndMarksTheActiveOne — the views group is the
// affordance a consumer would otherwise hand-roll.
//
// Falsification: in viewChipClass, always `return "filter-chip"` → RED.
// Falsification: drop the `len(d.Resource.Views) > 0` group from filterBar → RED.
func TestFilterBar_RendersViewChipsAndMarksTheActiveOne(t *testing.T) {
	d := listPageData{
		Resource: Resource{
			Name:       "revenue",
			Views:      []View{{Key: "week", Label: "This week"}, {Key: "month", Label: "This month"}},
			ViewsLabel: "period",
		},
		BasePath:   "/admin",
		ActiveView: "month",
		Selected:   url.Values{},
	}
	body := renderFilterBar(t, d)
	for _, want := range []string{`name="view"`, `value="week"`, `value="month"`, "This week", "This month", "period"} {
		if !strings.Contains(body, want) {
			t.Errorf("view chips missing %q in:\n%s", want, body)
		}
	}
	if got := chipClassOf(t, body, "view", "month"); got != "filter-chip active" {
		t.Errorf(`the selected view chip has class %q, want "filter-chip active"`, got)
	}
	if got := chipClassOf(t, body, "view", "week"); got != "filter-chip" {
		t.Errorf(`the unselected view chip has class %q, want "filter-chip"`, got)
	}
}

// TestFilterBar_KeepsTheSearchValueAcrossAChipSubmit — the chips are submit
// buttons on the same form as the search box. Without value= the input renders
// empty, so switching view silently discards a search the operator just typed
// and the table quietly widens.
//
// Falsification: drop `value={ d.Selected.Get(f.Key) }` from the input → RED.
func TestFilterBar_KeepsTheSearchValueAcrossAChipSubmit(t *testing.T) {
	d := listPageData{
		Resource: Resource{
			Name:   "revenue",
			Views:  []View{{Key: "week"}, {Key: "month"}},
			Filter: admintable.FilterSpec{Filters: []admintable.Filter{{Key: "q", SQLExprs: []string{"name"}, Match: admintable.ILike}}},
		},
		BasePath:   "/admin",
		ActiveView: "week",
		Selected:   url.Values{"q": {"академия"}},
	}
	if body := renderFilterBar(t, d); !strings.Contains(body, `value="академия"`) {
		t.Errorf("the search box lost its value; switching view would clear the operator's query:\n%s", body)
	}
}

// TestFilterBar_RendersForAViewOnlyResource — a resource with views and no
// filters still needs the bar, or its only control disappears.
//
// Falsification: revert filterBar's guard to `len(d.Resource.Filter.Filters) > 0` → RED.
func TestFilterBar_RendersForAViewOnlyResource(t *testing.T) {
	d := listPageData{
		Resource:   Resource{Name: "revenue", Views: []View{{Key: "week"}, {Key: "month"}}},
		BasePath:   "/admin",
		ActiveView: "week",
		Selected:   url.Values{},
	}
	if body := renderFilterBar(t, d); !strings.Contains(body, `name="view"`) {
		t.Errorf("a views-only resource rendered no filter bar:\n%s", body)
	}
}
