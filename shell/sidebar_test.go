package shell_test

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/shell"
)

var update = flag.Bool("update", false, "update golden test files")

// renderLayout is a test helper that renders Layout with the given context and
// nav slice, returning the full HTML string. Used by all sidebar tests.
func renderLayout(t *testing.T, ctx context.Context, nav []shell.NavItem) string {
	t.Helper()
	var b bytes.Buffer
	empty := templ.ComponentFunc(func(_ context.Context, _ io.Writer) error { return nil })
	if err := shell.Layout("T", nav, empty).Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// TestCollapsedCSSExists confirms the .sidebar.collapsed CSS rule is present
// in the rendered layout.
// Falsification: remove the .sidebar.collapsed block from styles.templ →
// this test fails with "no .sidebar.collapsed CSS rule rendered".
func TestCollapsedCSSExists(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, ".sidebar.collapsed") {
		t.Fatal("inert-collapse bug: no .sidebar.collapsed CSS rule rendered")
	}
}

// TestCollapsedClassSSR confirms the SSR-rendered aside carries class="sidebar collapsed"
// when ChromeFromContext reports Collapsed=true.
// Falsification: remove the templ.KV call in layout.templ → class stays "sidebar".
func TestCollapsedClassSSR(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Collapsed: true})
	html := renderLayout(t, ctx, nil)
	if !strings.Contains(html, `class="sidebar collapsed"`) {
		t.Fatal("collapsed chrome state not SSR-rendered as class (FOUC not killed)")
	}
}

// TestExpandedClassSSR confirms that with Collapsed=false, the aside does NOT
// get the collapsed class (baseline behavior is preserved).
func TestExpandedClassSSR(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Collapsed: false})
	html := renderLayout(t, ctx, nil)
	if strings.Contains(html, `class="sidebar collapsed"`) {
		t.Fatal("expanded sidebar must not carry .collapsed class")
	}
	if !strings.Contains(html, `class="sidebar"`) {
		t.Fatal("expanded aside missing expected class=\"sidebar\"")
	}
}

// TestActiveItemAriaCurrent confirms the active nav anchor carries aria-current="page".
// Falsification: remove `if item.Active { aria-current="page" }` from layout.templ →
// this test fails.
func TestActiveItemAriaCurrent(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x", Active: true}}
	html := renderLayout(t, context.Background(), nav)
	if !strings.Contains(html, `aria-current="page"`) {
		t.Fatal("active nav item missing aria-current")
	}
}

// TestInactiveItemNoAriaCurrent confirms an inactive nav item does NOT get
// aria-current (backward-compat).
func TestInactiveItemNoAriaCurrent(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x", Active: false}}
	html := renderLayout(t, context.Background(), nav)
	if strings.Contains(html, `aria-current`) {
		t.Fatal("inactive nav item must not have aria-current")
	}
}

// TestBadgeRenders confirms a non-nil Badge closure produces a .sidebar-count pill element.
// Falsification: remove the badge render block from layout.templ →
// the span element with sidebar-count is absent.
func TestBadgeRenders(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x",
		Badge: func(context.Context) string { return "12" }}}
	html := renderLayout(t, context.Background(), nav)
	if !strings.Contains(html, `<span class="sidebar-count">`) || !strings.Contains(html, ">12<") {
		t.Fatal("badge closure not rendered as .sidebar-count pill element")
	}
}

// TestBadgeEmptyStringNoPill confirms a Badge closure returning "" produces no pill element.
// Note: "sidebar-count" appears in the CSS rule; the element check uses the HTML span tag.
func TestBadgeEmptyStringNoPill(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x",
		Badge: func(context.Context) string { return "" }}}
	html := renderLayout(t, context.Background(), nav)
	if strings.Contains(html, `<span class="sidebar-count"`) {
		t.Fatal("empty Badge string must render no pill element")
	}
}

// TestBadgeNilNoPill confirms a nil Badge (zero value) produces no .sidebar-count element.
// This is the primary backward-compat test: existing callers with no Badge field set
// must see identical render output.
// Falsification: call item.Badge(ctx) unconditionally (nil dereference panic, or
// panic-recover turns it into empty string → still no pill, but test catches the panic).
func TestBadgeNilNoPill(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x"}}
	if strings.Contains(renderLayout(t, context.Background(), nav), `<span class="sidebar-count"`) {
		t.Fatal("nil Badge must render no pill (backward-compat)")
	}
}

// TestNoDataPm7SidebarOnAside is a fitness check: the shell <aside> must NOT
// carry data-pm7-sidebar, which would auto-instantiate PM7Sidebar and create
// dual state-machine authority over the sidebar (Decision 1, plan §Decision1).
// Falsification: add data-pm7-sidebar to the <aside> in layout.templ → FAIL.
func TestNoDataPm7SidebarOnAside(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if strings.Contains(html, "data-pm7-sidebar") {
		t.Fatal("fitness violation: aside carries data-pm7-sidebar (PM7Sidebar dual-authority, Decision 1)")
	}
}

// TestZeroValueNavItemBackwardCompat confirms that a NavItem with only the
// required ID/Label/URL fields (Badge=nil, Active=false, Group="", Icon="")
// renders identically to pre-Badge behavior: label present, no pill, no aria-current.
// Falsification: make Badge run even when nil → panic or spurious pill.
func TestZeroValueNavItemBackwardCompat(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "X", URL: "/admin/x"}}
	html := renderLayout(t, context.Background(), nav)
	if !strings.Contains(html, ">X<") {
		t.Fatal("nav item label missing (backward-compat regression)")
	}
	if strings.Contains(html, `<span class="sidebar-count"`) {
		t.Fatal("zero-value Badge produced a pill element (backward-compat regression)")
	}
	if strings.Contains(html, "aria-current") {
		t.Fatal("inactive zero-value item has aria-current (backward-compat regression)")
	}
}

// TestCachedBadgeDeduplicates confirms CachedBadge calls the underlying fn only
// once within the TTL window regardless of how many renders occur.
// Falsification: remove the expires check in CachedBadge → count > 1.
func TestCachedBadgeDeduplicates(t *testing.T) {
	var count int
	fn := shell.CachedBadge(5*time.Second, func(_ context.Context) string {
		count++
		return "42"
	})

	// Call three times within the TTL.
	for i := 0; i < 3; i++ {
		result := fn(context.Background())
		if result != "42" {
			t.Fatalf("call %d: expected '42', got %q", i+1, result)
		}
	}
	if count != 1 {
		t.Fatalf("expected fn called once within TTL, got %d calls", count)
	}
}

// ── Tooltip tests (Task 2.2) ──────────────────────────────────────────────

// TestTooltipAriaLabelAlways confirms that every sidebar link carries
// aria-label set to tooltipOf(item) (Tooltip when set, Label as fallback),
// regardless of collapsed/expanded state.
// Falsification: remove aria-label emission from layout.templ → FAIL.
func TestTooltipAriaLabelAlways(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "Items", Icon: "🗂", URL: "/admin/x",
		Tooltip: "Items overview"}}
	for _, collapsed := range []bool{false, true} {
		ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Collapsed: collapsed})
		html := renderLayout(t, ctx, nav)
		if !strings.Contains(html, `aria-label="Items overview"`) {
			t.Fatalf("collapsed=%v: sidebar link must carry aria-label=Tooltip value", collapsed)
		}
	}
}

// TestTooltipFallbackToLabel confirms that when NavItem.Tooltip is empty,
// aria-label falls back to Label.
// Falsification: hard-code aria-label="" → FAIL; remove fallback → FAIL.
func TestTooltipFallbackToLabel(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "Items", Icon: "🗂", URL: "/admin/x"}}
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Collapsed: true})
	html := renderLayout(t, ctx, nav)
	if !strings.Contains(html, `aria-label="Items"`) {
		t.Fatal("empty Tooltip must fall back to Label for aria-label")
	}
}

// TestTooltipSpanPresent confirms every sidebar link always carries a
// span.sidebar-tooltip element containing tooltipOf(item); CSS controls
// visibility (hidden in expanded, shown on hover in collapsed).
// Falsification: remove the span.sidebar-tooltip from layout.templ → FAIL.
func TestTooltipSpanPresent(t *testing.T) {
	nav := []shell.NavItem{{ID: "x", Label: "Items", Icon: "🗂", URL: "/admin/x",
		Tooltip: "Items overview"}}
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Collapsed: true})
	html := renderLayout(t, ctx, nav)
	if !strings.Contains(html, `class="sidebar-tooltip"`) {
		t.Fatal("sidebar link must carry span.sidebar-tooltip")
	}
	if !strings.Contains(html, ">Items overview<") {
		t.Fatal("span.sidebar-tooltip must contain the tooltip text")
	}
}

// TestTooltipCSSHiddenByDefault confirms the rendered CSS hides .sidebar-tooltip
// by default (expanded mode — tooltip invisible when sidebar is open).
// Falsification: remove .sidebar-tooltip{display:none} from styles.templ → FAIL.
func TestTooltipCSSHiddenByDefault(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, ".sidebar-tooltip{display:none}") {
		t.Fatal("CSS must hide .sidebar-tooltip by default (expanded mode wayfinding off)")
	}
}

// TestTooltipCSSShowOnCollapsedHover confirms the CSS reveals .sidebar-tooltip
// on hover in collapsed mode — pure CSS, CSP-clean, no JS required.
// Falsification: remove the hover rule from styles.templ → FAIL.
func TestTooltipCSSShowOnCollapsedHover(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, ".sidebar.collapsed .sidebar-item:hover .sidebar-tooltip") {
		t.Fatal("CSS must reveal .sidebar-tooltip on hover when sidebar is collapsed")
	}
}

// TestTooltipCSSFixedPositioning confirms the tooltip CSS uses position:fixed
// (not position:absolute) to escape the .sidebar{overflow:hidden} clip context.
// position:absolute is confined to the containing block and clipped by every
// overflow:hidden ancestor; position:fixed is clipped only by ancestors that
// establish a fixed-position containing block (transform/filter/perspective),
// none of which exist on .sidebar.
// Falsification: revert to position:absolute in styles.templ → FAIL.
func TestTooltipCSSFixedPositioning(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, "position:fixed") {
		t.Fatal("tooltip CSS must use position:fixed to escape .sidebar{overflow:hidden} clip; position:absolute is always clipped")
	}
}

// TestTooltipCSSAnchorPositioning confirms the tooltip CSS uses CSS anchor
// positioning (anchor(right), anchor(top)) so each tooltip appears adjacent
// to its hovered sidebar item even though position:fixed positions relative
// to the viewport rather than the item.
// Without anchor(), all tooltips would stack at left:56px;top:0 (wrong).
// Falsification: remove anchor(right) from styles.templ → FAIL.
func TestTooltipCSSAnchorPositioning(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, "anchor(right)") {
		t.Fatal("tooltip CSS must use CSS anchor positioning anchor(right) for horizontal placement adjacent to the hovered item")
	}
	if !strings.Contains(html, "anchor(top)") {
		t.Fatal("tooltip CSS must use CSS anchor positioning anchor(top) for vertical alignment with the hovered item")
	}
}

// TestTooltipAnchorNameOnNavItem confirms every sidebar nav link carries a CSS
// anchor-name inline style (anchor-name: --nav-<id>) enabling the tooltip to
// reference it via position-anchor, and that the .sidebar-tooltip span carries
// the matching position-anchor value.
// Falsification: remove the inline style from the <a> or <span> in layout.templ → FAIL.
func TestTooltipAnchorNameOnNavItem(t *testing.T) {
	nav := []shell.NavItem{{ID: "dash", Label: "Dashboard", URL: "/admin/dash"}}
	html := renderLayout(t, context.Background(), nav)
	if !strings.Contains(html, "anchor-name: --nav-dash") {
		t.Fatal("nav item <a> must carry style=\"anchor-name: --nav-<id>\" for CSS anchor positioning")
	}
	if !strings.Contains(html, "position-anchor: --nav-dash") {
		t.Fatal("tooltip <span> must carry style=\"position-anchor: --nav-<id>\" referencing the item anchor")
	}
}

// TestLogoutTooltipWayfinding confirms the hardcoded Logout link carries both
// aria-label and a .sidebar-tooltip span with a CSS anchor-name, consistent
// with nav items.  Without this the Logout bare-arrow glyph has no collapsed-
// mode wayfinding — a gap singled out by the code-quality reviewer.
// Falsification: remove aria-label or the sidebar-tooltip span from the Logout
// link in layout.templ → FAIL.
func TestLogoutTooltipWayfinding(t *testing.T) {
	// Render with no nav so the only content is the fixed Logout link.
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, `aria-label="Logout"`) {
		t.Fatal("Logout link must carry aria-label=\"Logout\" for collapsed-mode screen-reader wayfinding")
	}
	if !strings.Contains(html, "anchor-name: --nav-logout") {
		t.Fatal("Logout link must carry style=\"anchor-name: --nav-logout\" for CSS anchor positioning")
	}
	if !strings.Contains(html, `class="sidebar-tooltip"`) {
		t.Fatal("Logout link must carry span.sidebar-tooltip for collapsed-mode visual wayfinding")
	}
}

// ── End tooltip tests ─────────────────────────────────────────────────────

// TestChrome_ZeroFields_GoldenStable locks the HTML output produced when
// Layout receives a zero ChromeState (Collapsed=false, CollapsedGroups=nil,
// Profile=zero).  This is the baseline every subsequent Phase must not break.
//
// Run with -update to regenerate the golden file after an intentional change.
//
// Falsification: revert ChromeFromContext(ctx).Collapsed back to
// SidebarFromContext(ctx).Collapsed in layout_templ.go → compile error (symbol
// gone) → test fails to build.  Alternatively, removing the chromeCtxKey lookup
// so ChromeFromContext always returns zero means the Collapsed=true test above
// also fails (see TestCollapsedClassSSR).
func TestChrome_ZeroFields_GoldenStable(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{})
	got := renderLayout(t, ctx, nil)

	goldenPath := filepath.Join("testdata", "chrome_zero_golden.html")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file missing; regenerate with: go test ./shell/ -update\n%v", err)
	}
	if string(want) != got {
		t.Fatalf("rendered HTML differs from golden %s\nRefresh with: go test ./shell/ -update", goldenPath)
	}
}
