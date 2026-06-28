package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
)

// TestGrid_WrapperAndOrder asserts that Grid renders exactly one .stat-grid
// div that wraps all child components in the given order.
//
// Falsification: remove the stat-grid wrapper from Grid → first assertion
// fails; reverse the widget order → order assertion fails; add an extra
// stat-grid wrapper → duplicate check fails.
func TestGrid_WrapperAndOrder(t *testing.T) {
	a := components.StatCard{Label: "Alpha", Value: "1"}
	b := components.StatCard{Label: "Beta", Value: "2"}

	var buf bytes.Buffer
	err := components.Grid(
		components.StatCardView(a),
		components.StatCardView(b),
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	// Exactly one .stat-grid wrapper.
	count := strings.Count(html, `class="stat-grid"`)
	if count != 1 {
		t.Errorf("expected exactly 1 class=stat-grid; got %d in:\n%s", count, html)
	}

	// Both cards present.
	if !strings.Contains(html, "Alpha") {
		t.Errorf("missing card Alpha; got:\n%s", html)
	}
	if !strings.Contains(html, "Beta") {
		t.Errorf("missing card Beta; got:\n%s", html)
	}

	// Alpha appears before Beta (order preserved).
	idxAlpha := strings.Index(html, "Alpha")
	idxBeta := strings.Index(html, "Beta")
	if idxAlpha >= idxBeta {
		t.Errorf("expected Alpha before Beta; Alpha at %d, Beta at %d in:\n%s", idxAlpha, idxBeta, html)
	}

	// Both cards are inside the stat-grid wrapper.
	gridOpen := strings.Index(html, `class="stat-grid"`)
	gridClose := strings.LastIndex(html, "</div>")
	if gridOpen < 0 || gridClose < 0 || idxAlpha < gridOpen || idxBeta > gridClose {
		t.Errorf("cards not inside stat-grid wrapper; got:\n%s", html)
	}
}

// TestGrid_Empty asserts that Grid with no widgets renders an empty
// .stat-grid wrapper without panicking.
//
// Falsification: panic or omit wrapper when widgets slice is empty → test fails.
func TestGrid_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := components.Grid().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render empty grid: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `class="stat-grid"`) {
		t.Errorf("empty Grid must still render .stat-grid wrapper; got:\n%s", html)
	}
}
