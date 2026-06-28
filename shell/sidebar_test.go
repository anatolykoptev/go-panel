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

// ── Collapsible group tests (Phase 4) ────────────────────────────────────

// TestCollapsedGroupSSR confirms that a group whose name appears in
// ChromeState.CollapsedGroups renders with data-collapsed="true" on the
// sidebar-group div (server-side, kills the flash-of-expanded-group).
// Falsification: remove the `data-collapsed="true"` emission from layout.templ
// OR stop reading CollapsedGroups in toNavGroups → attribute absent → FAIL.
//
// Note: CSS selectors also contain [data-collapsed="true"]; we check for the
// HTML-attribute form " data-collapsed=" (space-prefixed, no bracket) to
// distinguish element attribute from CSS selector text.
func TestCollapsedGroupSSR(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Content"},
		{ID: "posts", Label: "Posts", URL: "/admin/posts"},
	}
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{
		CollapsedGroups: map[string]bool{"Content": true},
	})
	html := renderLayout(t, ctx, nav)
	// " data-collapsed=" = HTML attribute; "[data-collapsed=" = CSS selector
	if !strings.Contains(html, ` data-collapsed="true"`) {
		t.Fatal("collapsed group must render data-collapsed=\"true\" server-side (no flash)")
	}
}

// TestNonCollapsedGroupNoAttribute confirms that a group NOT in CollapsedGroups
// does NOT render data-collapsed (baseline preserved; prevents false-collapsed state).
// Falsification: always emit data-collapsed on every group → FAIL.
func TestNonCollapsedGroupNoAttribute(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Content"},
		{ID: "posts", Label: "Posts", URL: "/admin/posts"},
	}
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{
		CollapsedGroups: nil, // no groups collapsed
	})
	html := renderLayout(t, ctx, nav)
	// " data-collapsed=" = HTML attribute (CSS selector form has no leading space)
	if strings.Contains(html, ` data-collapsed=`) {
		t.Fatal("non-collapsed group must not carry data-collapsed attribute")
	}
}

// TestGroupLabelIsButton confirms the group label renders as a <button> element
// with a data-group attribute and aria-expanded for accessibility.
// Checks are split: expanded-group → aria-expanded="true"; no tight string match
// so attribute ordering doesn't fragment the test.
// Falsification: revert group label to <div> → no <button> → FAIL.
//
// Note: aria-expanded="true" is expected here because the group is not in CollapsedGroups.
func TestGroupLabelIsButton(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Settings"},
		{ID: "cfg", Label: "Config", URL: "/admin/cfg"},
	}
	html := renderLayout(t, context.Background(), nav)
	if !strings.Contains(html, `<button type="button"`) {
		t.Fatal("group header must be a <button> element")
	}
	if !strings.Contains(html, `class="sidebar-group-label"`) {
		t.Fatal("group button must carry sidebar-group-label class")
	}
	if !strings.Contains(html, `data-group="Settings"`) {
		t.Fatal("group button must carry data-group attribute with the group name")
	}
	if !strings.Contains(html, `aria-expanded="true"`) {
		t.Fatal("expanded group button must carry aria-expanded=\"true\" for accessibility")
	}
}

// TestCollapsedGroupButtonAriaExpanded confirms that a SSR-collapsed group button
// carries aria-expanded="false" and an expanded group carries aria-expanded="true".
// Falsification: remove the aria-expanded attribute from layout.templ → FAIL.
func TestCollapsedGroupButtonAriaExpanded(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Content"},
		{ID: "posts", Label: "Posts", URL: "/admin/posts"},
	}
	// collapsed → aria-expanded="false"
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{
		CollapsedGroups: map[string]bool{"Content": true},
	})
	html := renderLayout(t, ctx, nav)
	if !strings.Contains(html, `aria-expanded="false"`) {
		t.Fatal("SSR-collapsed group button must carry aria-expanded=\"false\"")
	}
	if strings.Contains(html, `aria-expanded="true"`) {
		t.Fatal("collapsed group must not carry aria-expanded=\"true\"")
	}

	// expanded (not in CollapsedGroups) → aria-expanded="true"
	ctx2 := shell.ContextWithChrome(context.Background(), shell.ChromeState{
		CollapsedGroups: nil,
	})
	html2 := renderLayout(t, ctx2, nav)
	if !strings.Contains(html2, `aria-expanded="true"`) {
		t.Fatal("expanded group button must carry aria-expanded=\"true\"")
	}
	if strings.Contains(html2, `aria-expanded="false"`) {
		t.Fatal("expanded group must not carry aria-expanded=\"false\"")
	}
}

// TestActiveGroupForcedExpanded confirms that a group containing the active nav
// item is never rendered collapsed even when present in CollapsedGroups.
// Falsification: remove the "if item.Active { out[last].Collapsed = false }" guard
// in toNavGroups → collapsed=true survives → active wayfinding hidden → FAIL.
//
// JS contract: groupsApply() in admin.js mirrors this server guard by checking
// group.querySelector('.sidebar-item.active') before applying data-collapsed from
// the sb-g cookie. Both sides MUST stay in lockstep — if only one enforces the
// invariant, a page reload or htmx:afterSwap can collapse the active group and
// hide the current-page link (this test catches the server half; smoke-test the
// JS half manually by loading a page with sb-g set to the active group's name).
func TestActiveGroupForcedExpanded(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Content"},
		{ID: "posts", Label: "Posts", URL: "/admin/posts", Active: true},
	}
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{
		CollapsedGroups: map[string]bool{"Content": true},
	})
	html := renderLayout(t, ctx, nav)
	// Group contains active item → must NOT have data-collapsed attribute
	if strings.Contains(html, ` data-collapsed=`) {
		t.Fatal("group with active item must be forced expanded even when in CollapsedGroups (active wayfinding must not be hidden)")
	}
	// aria-expanded must be "true" (expanded)
	if strings.Contains(html, `aria-expanded="false"`) {
		t.Fatal("group button containing active item must carry aria-expanded=\"true\"")
	}
}

// TestGroupItemsNested confirms that nav items belonging to a group are rendered
// inside a .sidebar-group-items container (required for CSS max-height transition).
// Falsification: remove the sidebar-group-items wrapper div from layout.templ → FAIL.
func TestGroupItemsNested(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Content"},
		{ID: "posts", Label: "Posts", URL: "/admin/posts"},
		{ID: "pages", Label: "Pages", URL: "/admin/pages"},
	}
	html := renderLayout(t, context.Background(), nav)
	if !strings.Contains(html, `class="sidebar-group-items"`) {
		t.Fatal("group items must be wrapped in a .sidebar-group-items container for CSS transitions")
	}
}

// TestGroupCollapseCSSExists confirms the CSS rules for collapsed groups are
// rendered (max-height transition borrowed from pm7).
// Falsification: remove the group collapse CSS from styles.templ → FAIL.
func TestGroupCollapseCSSExists(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, ".sidebar-group[data-collapsed") {
		t.Fatal("group collapse CSS rule must be present (pm7-borrowed max-height transition)")
	}
	if !strings.Contains(html, ".sidebar-group-items") {
		t.Fatal("sidebar-group-items CSS rule must be present for transition container")
	}
}

// TestOnlyCollapsedGroupGetsAttribute confirms that when multiple groups exist
// and only one is in CollapsedGroups, only that group carries data-collapsed.
// Falsification: apply data-collapsed to all groups → second group fails → FAIL.
func TestOnlyCollapsedGroupGetsAttribute(t *testing.T) {
	nav := []shell.NavItem{
		{Group: "Alpha"},
		{ID: "a1", Label: "A1", URL: "/admin/a1"},
		{Group: "Beta"},
		{ID: "b1", Label: "B1", URL: "/admin/b1"},
	}
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{
		CollapsedGroups: map[string]bool{"Alpha": true},
	})
	html := renderLayout(t, ctx, nav)
	// Alpha → collapsed
	if !strings.Contains(html, `data-group="Alpha"`) {
		t.Fatal("Alpha group button must be present")
	}
	// Check HTML-attribute form (space-prefixed), not CSS-selector form (bracket-prefixed)
	if !strings.Contains(html, ` data-collapsed="true"`) {
		t.Fatal("Alpha group must have data-collapsed HTML attribute")
	}
	// Beta → not collapsed; count HTML attribute occurrences only
	occurrences := strings.Count(html, ` data-collapsed="true"`)
	if occurrences != 1 {
		t.Fatalf("expected exactly one data-collapsed=\"true\" HTML attribute, got %d", occurrences)
	}
}

// ── End collapsible group tests ───────────────────────────────────────────

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
