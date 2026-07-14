package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
)

// TestStatCardView_BasicFields asserts that StatCardView renders the expected
// class attributes and escaped field values.
//
// Falsification: remove the "stat-card" class from StatCardView → first
// assertion fails; remove "stat-label" class → second fails; remove
// "stat-value" class → third fails; remove delta-up span → fourth fails.
func TestStatCardView_BasicFields(t *testing.T) {
	c := components.StatCard{
		Label: "Total Jobs",
		Value: "1,284",
		Delta: "+12%",
		Trend: components.TrendUp,
	}
	var buf bytes.Buffer
	if err := components.StatCardView(c).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `class="stat-card"`) {
		t.Errorf("missing class=stat-card; got:\n%s", html)
	}
	if !strings.Contains(html, `class="stat-label"`) {
		t.Errorf("missing class=stat-label; got:\n%s", html)
	}
	if !strings.Contains(html, "Total Jobs") {
		t.Errorf("missing label text; got:\n%s", html)
	}
	if !strings.Contains(html, `class="stat-value"`) {
		t.Errorf("missing class=stat-value; got:\n%s", html)
	}
	if !strings.Contains(html, "1,284") {
		t.Errorf("missing value text; got:\n%s", html)
	}
	if !strings.Contains(html, "delta delta-up") {
		t.Errorf("missing delta delta-up class; got:\n%s", html)
	}
	if !strings.Contains(html, "+12%") {
		t.Errorf("missing delta text; got:\n%s", html)
	}
}

// TestStatCardView_XSSEscape asserts that templ auto-escapes HTML in Label,
// preventing XSS. templ.Raw is never used in StatCardView.
//
// Falsification: use templ.Raw in the label render path → the raw <script>
// tag appears unescaped and this test fails.
func TestStatCardView_XSSEscape(t *testing.T) {
	c := components.StatCard{
		Label: "<script>x</script>",
		Value: "0",
	}
	var buf bytes.Buffer
	if err := components.StatCardView(c).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if strings.Contains(html, "<script>") {
		t.Errorf("XSS: unescaped <script> in output; got:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected &lt;script&gt; escaped in output; got:\n%s", html)
	}
}

// TestStatCardView_TrendNone asserts that a zero-Trend card renders no delta span.
//
// Falsification: always render a delta span regardless of Trend → this test
// fails when it finds a "delta" class in the output.
func TestStatCardView_TrendNone(t *testing.T) {
	c := components.StatCard{
		Label: "Idle",
		Value: "0",
		Trend: components.TrendNone,
	}
	var buf bytes.Buffer
	if err := components.StatCardView(c).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `class="delta`) {
		t.Errorf("expected no delta span for TrendNone; got:\n%s", html)
	}
}

// TestStatCardView_TrendVariants exercises each Trend value and asserts the
// correct CSS modifier class is emitted.
//
// Falsification: change trendClass to always return "delta-up" → Down/Flat/New
// subtests fail.
func TestStatCardView_TrendVariants(t *testing.T) {
	cases := []struct {
		trend     components.Trend
		wantClass string
	}{
		{components.TrendUp, "delta-up"},
		{components.TrendDown, "delta-down"},
		{components.TrendFlat, "delta-flat"},
		{components.TrendNew, "delta-new"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.wantClass, func(t *testing.T) {
			c := components.StatCard{
				Label: "x",
				Value: "1",
				Delta: "∆",
				Trend: tc.trend,
			}
			var buf bytes.Buffer
			if err := components.StatCardView(c).Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			html := buf.String()
			if !strings.Contains(html, tc.wantClass) {
				t.Errorf("trend %v: want class %q; got:\n%s", tc.trend, tc.wantClass, html)
			}
		})
	}
}
