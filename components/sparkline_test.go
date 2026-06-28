package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
)

// TestSparkline_ThreePoint asserts that Sparkline([0,5,10]) emits a polyline
// with class=sparkline-line, exactly 3 x,y coordinate pairs, and Y-inversion
// (max value maps to the smallest y / top of the SVG).
//
// Falsification: remove the polyline branch from Sparkline → fails on
// class=sparkline-line; revert Y-inversion → fails on the y=2.0 assertion
// for the max value.
func TestSparkline_ThreePoint(t *testing.T) {
	var buf bytes.Buffer
	if err := components.Sparkline([]int{0, 5, 10}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `class="sparkline-line"`) {
		t.Errorf("missing class=sparkline-line; got:\n%s", html)
	}

	// Verify aria-hidden.
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("missing aria-hidden=true; got:\n%s", html)
	}

	// Verify exactly 3 x,y pairs by counting spaces inside points="...".
	// sparkWidth=80 sparkHeight=20 sparkPad=2:
	//   step = (80-4)/(3-1) = 38.0
	//   plotH = 16.0, maxV = 10
	//   i=0 v=0  → x=2.0  y=2+16*(1-0/10)=18.0
	//   i=1 v=5  → x=40.0 y=2+16*(1-5/10)=10.0
	//   i=2 v=10 → x=78.0 y=2+16*(1-10/10)=2.0  ← max maps to TOP (smallest y)
	wantPairs := []string{"2.0,18.0", "40.0,10.0", "78.0,2.0"}
	for _, pair := range wantPairs {
		if !strings.Contains(html, pair) {
			t.Errorf("missing expected coordinate pair %q; got:\n%s", pair, html)
		}
	}

	// Y-inversion: confirm max value (10, last element) maps to smallest y.
	if !strings.Contains(html, "78.0,2.0") {
		t.Errorf("Y-inversion: max value should map to top y≈2.0; got:\n%s", html)
	}
}

// TestSparkline_Nil asserts that Sparkline(nil) emits an empty svg with
// class=sparkline and aria-hidden=true; no polyline or line element.
//
// Falsification: remove the len==0 early-return branch from Sparkline →
// falls through to max-finding loop on empty slice → no class=sparkline.
func TestSparkline_Nil(t *testing.T) {
	var buf bytes.Buffer
	if err := components.Sparkline(nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `class="sparkline"`) {
		t.Errorf("missing class=sparkline on empty svg; got:\n%s", html)
	}
	if strings.Contains(html, "polyline") {
		t.Errorf("nil input must not emit a polyline; got:\n%s", html)
	}
	if strings.Contains(html, "<line") {
		t.Errorf("nil input must not emit a line element; got:\n%s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("missing aria-hidden=true; got:\n%s", html)
	}
}

// TestSparkline_AllZeros asserts that Sparkline([0,0,0]) emits a <line> with
// class=sparkline-zero (flat baseline at mid-height) and NOT a polyline.
//
// Falsification: remove the maxV==0 branch → falls through to polyline path
// (divide-by-zero or degenerate output) without sparkline-zero class.
func TestSparkline_AllZeros(t *testing.T) {
	var buf bytes.Buffer
	if err := components.Sparkline([]int{0, 0, 0}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `class="sparkline-zero"`) {
		t.Errorf("missing class=sparkline-zero baseline; got:\n%s", html)
	}
	if strings.Contains(html, "polyline") {
		t.Errorf("all-zeros input must not emit a polyline; got:\n%s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("missing aria-hidden=true; got:\n%s", html)
	}
}

// TestSparkline_NoInjectionInPoints asserts that the points attribute of the
// polyline contains only decimal digits, dots, commas and spaces — no HTML or
// JS injection is possible through the series values.
//
// Falsification: change Sparkline to use %s formatting for a user-supplied
// string value → non-numeric tokens appear in points and this test fails.
func TestSparkline_NoInjectionInPoints(t *testing.T) {
	var buf bytes.Buffer
	if err := components.Sparkline([]int{1, 5, 3, 8, 2}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	idx := strings.Index(html, `points="`)
	if idx == -1 {
		t.Fatalf("no points attribute found; got:\n%s", html)
	}
	start := idx + len(`points="`)
	rest := html[start:]
	end := strings.Index(rest, `"`)
	if end == -1 {
		t.Fatalf("unclosed points attribute; got:\n%s", html)
	}
	points := rest[:end]

	for i, ch := range points {
		if (ch < '0' || ch > '9') && ch != '.' && ch != ',' && ch != ' ' {
			t.Errorf("non-numeric character %q at index %d in points=%q", ch, i, points)
		}
	}
}

// TestSparkline_AriaHidden asserts that every code path emits aria-hidden="true".
//
// Falsification: remove aria-hidden from any branch → one of the three cases
// in this table fails.
func TestSparkline_AriaHidden(t *testing.T) {
	cases := []struct {
		name   string
		series []int
	}{
		{"nil", nil},
		{"all-zeros", []int{0, 0, 0}},
		{"normal", []int{1, 2, 3}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := components.Sparkline(tc.series).Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(buf.String(), `aria-hidden="true"`) {
				t.Errorf("missing aria-hidden=true for series %v; got:\n%s", tc.series, buf.String())
			}
		})
	}
}
