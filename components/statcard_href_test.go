package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
)

func statCardHTML(t *testing.T, c components.StatCard) string {
	t.Helper()
	var buf bytes.Buffer
	if err := components.StatCardView(c).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// A card with a safe Href is one anchor that wraps the label AND the value
// (the anchor's accessible name), with no nested link.
func TestStatCardView_HrefWrapsCard(t *testing.T) {
	out := statCardHTML(t, components.StatCard{
		Label: "Renewals", Value: "3", Delta: "5", Trend: components.TrendUp, Href: "/admin/leases?due=1&x=2",
	})
	if n := strings.Count(out, "<a "); n != 1 {
		t.Fatalf("want exactly 1 anchor, got %d: %s", n, out)
	}
	if !strings.Contains(out, `<a class="stat-card stat-card-link" href="/admin/leases?due=1&amp;x=2">`) {
		t.Errorf("anchor/href wrong: %s", out)
	}
	inner := out[strings.Index(out, ">")+1 : strings.Index(out, "</a>")]
	for _, want := range []string{"Renewals", ">3", "5"} {
		if !strings.Contains(inner, want) {
			t.Errorf("anchor content lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, `<div class="stat-card"`) {
		t.Errorf("linked card must not also render a div card: %s", out)
	}
}

// Without Href the markup is byte-identical to the pre-Href card.
func TestStatCardView_NoHrefUnchanged(t *testing.T) {
	out := statCardHTML(t, components.StatCard{Label: "Seats", Value: "6 / 42", Delta: "1", Trend: components.TrendDown})
	want := `<div class="stat-card"><div class="stat-label">Seats</div><div class="stat-value">6 / 42 <span class="delta delta-down">↓1</span></div></div>`
	if out != want {
		t.Errorf("markup drifted:\n got: %s\nwant: %s", out, want)
	}
	if strings.Contains(out, "<a ") {
		t.Errorf("no Href must render no anchor: %s", out)
	}
}

// Href is consumer data: only a same-origin path becomes a link.
// Mutation: unitstrip.go safeUnitHref body -> `return true` -> the unsafe rows
// below go RED (called from statcard.templ StatCardView and kpistrip.templ KPIStrip).
func TestStatCardView_UnsafeHrefRendersNoAnchor(t *testing.T) {
	for _, h := range []string{
		"javascript:alert(1)", "//evil.example", `/\evil.example`, "https://x", "admin/x",
		"/\t/evil.example", "/\n/evil.example",
	} {
		out := statCardHTML(t, components.StatCard{Label: "L", Value: "V", Href: h})
		if strings.Contains(out, "<a ") || strings.Contains(out, "href=") {
			t.Errorf("href %q rendered a link: %s", h, out)
		}
		if !strings.Contains(out, `<div class="stat-card">`) || !strings.Contains(out, "V") {
			t.Errorf("href %q: plain card missing: %s", h, out)
		}
	}
}

func TestKPIStrip_Href(t *testing.T) {
	var buf bytes.Buffer
	cells := []components.KPI{
		{Label: "Leads", Value: "15", Href: "/admin/leads"},
		{Label: "Bad", Value: "9", Href: "javascript:alert(1)"},
		{Label: "Plain", Value: "1"},
	}
	if err := components.KPIStrip(cells).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if n := strings.Count(out, "<a "); n != 1 {
		t.Fatalf("want 1 anchor, got %d: %s", n, out)
	}
	if !strings.Contains(out, `<a class="kpi-cell kpi-cell-link" href="/admin/leads"><div class="kpi-label">Leads</div><div class="kpi-value"><span>15</span>`) {
		t.Errorf("linked cell wrong: %s", out)
	}
	if !strings.Contains(out, `<div class="kpi-cell"><div class="kpi-label">Plain</div><div class="kpi-value"><span>1</span> </div></div>`) {
		t.Errorf("plain cell drifted: %s", out)
	}
	if strings.Count(out, `<div class="kpi-cell">`) != 2 || strings.Contains(out, "javascript:") {
		t.Errorf("unsafe/plain cells wrong: %s", out)
	}
}
