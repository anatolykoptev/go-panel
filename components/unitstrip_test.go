package components_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
)

func renderUnits(t *testing.T, units ...components.Unit) string {
	t.Helper()
	return render(t, components.UnitStrip(components.UnitRow{Label: "Places", Units: units}))
}

// Each tone carries a fixed class, a glyph and a text label; colour alone never
// distinguishes them. The visible label is in the legend, the tile carries it
// as screen-reader text.
// Mutation: unitstrip.go unitGlyph `case UnitHold:` returns "●" -> RED on glyph.
func TestUnitStrip_EveryToneHasClassGlyphAndLabel(t *testing.T) {
	cases := []struct {
		tone  components.UnitTone
		class string
		glyph string
		label string
	}{
		{components.UnitFree, "unit-free", "○", "Free"},
		{components.UnitHold, "unit-hold", "◌", "Hold"},
		{components.UnitSale, "unit-sale", "●", "Sold"},
		{components.UnitRenewalDue, "unit-renewal", "↻", "Renewal due"},
		{components.UnitUnverified, "unit-unverified", "?", "Under review"},
		{components.UnitConflict, "unit-conflict", "!", "Conflict"},
	}
	legend := render(t, components.UnitLegend(components.UnitLabels{}))
	for _, c := range cases {
		out := renderUnits(t, components.Unit{Num: 3, Tone: c.tone})
		if !strings.Contains(out, `class="unit-tile `+c.class+`"`) {
			t.Errorf("%s: class missing in %s", c.class, out)
		}
		if !strings.Contains(out, `<span class="unit-glyph" aria-hidden="true">`+c.glyph+`</span>`) {
			t.Errorf("%s: glyph %q missing in %s", c.class, c.glyph, out)
		}
		if !strings.Contains(out, `<span class="occ-sr">Place 3 · `+c.label+`</span>`) {
			t.Errorf("%s: sr label %q missing in %s", c.class, c.label, out)
		}
		if !strings.Contains(legend, `unit-tile `+c.class+`" aria-hidden="true">`+c.glyph+`</span> `+c.label+`</li>`) {
			t.Errorf("%s: legend entry (glyph %q, label %q) missing in %s", c.class, c.glyph, c.label, legend)
		}
	}
}

// The zero tone is Free, but a value outside the closed set must never read as
// free: an unclassifiable unit is "under review", not bookable.
func TestUnitStrip_UnknownToneIsNeverFree(t *testing.T) {
	out := renderUnits(t, components.Unit{Num: 1, Tone: components.UnitTone(99)}, components.Unit{Num: 2})
	if strings.Count(out, `unit-tile unit-unverified"`) != 1 || strings.Count(out, `unit-tile unit-free"`) != 1 {
		t.Errorf("want one unverified (tone 99) and one free (zero value): %s", out)
	}
}

// Note and Title are plain text: an HTML payload is escaped, never markup, and
// a quote in Title cannot break out of the title attribute.
// Mutation: unitstrip.templ `{ u.Note }` -> `@templ.Raw(u.Note)` -> RED.
func TestUnitStrip_NoteAndTitleAreEscaped(t *testing.T) {
	out := renderUnits(t, components.Unit{
		Num: 1, Tone: components.UnitHold, Note: "<script>alert(1)</script>",
		Title: `Place 1 · "quoted" onmouseover="x"`,
	})
	if strings.Contains(out, "<script>") {
		t.Errorf("unescaped note: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("note not shown escaped: %s", out)
	}
	if !strings.Contains(out, `title="Place 1 · &#34;quoted&#34; onmouseover=&#34;x&#34;"`) {
		t.Errorf("title not escaped in attribute: %s", out)
	}
}

// An empty Title falls back to place, tone and note so a tile always has a name.
func TestUnitStrip_DefaultTitleAndLabels(t *testing.T) {
	out := renderUnits(t, components.Unit{Num: 7, Tone: components.UnitHold, Note: "to 12.11"})
	if !strings.Contains(out, `title="Place 7 · Hold · to 12.11"`) {
		t.Errorf("default title: %s", out)
	}
	out = render(t, components.UnitStrip(components.UnitRow{
		Label:  "Места",
		Labels: components.UnitLabels{Place: "Место", Hold: "Бронь"},
		Units:  []components.Unit{{Num: 7, Tone: components.UnitHold}},
	}))
	if !strings.Contains(out, `title="Место 7 · Бронь"`) || !strings.Contains(out, `aria-label="Места"`) {
		t.Errorf("localised labels: %s", out)
	}
}

// Href is consumer data: only a same-origin path becomes a link.
// Mutation: unitstrip.go safeUnitHref body -> `return true` -> the unsafe rows
// below go RED (also TestUnitStripCell_UnsafeHrefDropsLink in resource).
func TestUnitStrip_HrefSafety(t *testing.T) {
	cases := []struct {
		href string
		link bool
	}{
		{"/admin/leases/7", true},
		{"/admin/leases?id=7&x=1", true},
		{"/", true},
		{"", false},
		{"javascript:alert(1)", false},
		{"//evil.example", false},
		{`/\evil`, false},
		{"https://x", false},
		{"http://x/admin", false},
		{"admin/leases/7", false},
		{"/\t/evil.example", false},
		{"/\n/evil.example", false},
		{"/ok\r\nX: y", false},
	}
	for _, c := range cases {
		out := renderUnits(t, components.Unit{Num: 1, Tone: components.UnitSale, Href: c.href})
		if got := strings.Contains(out, "<a "); got != c.link {
			t.Errorf("href %q: anchor=%v want %v: %s", c.href, got, c.link, out)
		}
		if !c.link && strings.Contains(out, "href=") {
			t.Errorf("href %q leaked an href attribute: %s", c.href, out)
		}
		if c.link && !strings.Contains(out, `href="`+strings.ReplaceAll(c.href, "&", "&amp;")+`"`) {
			t.Errorf("href %q not rendered verbatim: %s", c.href, out)
		}
	}
}
