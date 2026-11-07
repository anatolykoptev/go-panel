package resource_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/resource"
)

// The cell helper is what a consumer calls: an unsafe href must not reach the
// list cell as a link, and a note must not reach it as markup.
func TestUnitStripCell_UnsafeHrefDropsLink(t *testing.T) {
	for _, href := range []string{"javascript:alert(1)", "//evil.example", `/\evil`, "https://x"} {
		out := resource.UnitStripCell(components.UnitRow{Label: "P", Units: []components.Unit{
			{Num: 1, Tone: components.UnitSale, Href: href, Note: "<b>x</b>"},
		}})
		if strings.Contains(out, "<a ") || strings.Contains(out, "href=") {
			t.Errorf("href %q rendered a link: %s", href, out)
		}
		if strings.Contains(out, "<b>") {
			t.Errorf("note not escaped: %s", out)
		}
	}
	out := resource.UnitStripCell(components.UnitRow{Label: "P", Units: []components.Unit{
		{Num: 1, Tone: components.UnitSale, Href: "/admin/leases/7"},
	}})
	if !strings.Contains(out, `<a class="unit-tile unit-sale" href="/admin/leases/7"`) {
		t.Errorf("same-origin href not linked: %s", out)
	}
	if !strings.HasPrefix(out, `<ul class="unit-strip"`) {
		t.Errorf("not a bare fragment: %s", out)
	}
}
