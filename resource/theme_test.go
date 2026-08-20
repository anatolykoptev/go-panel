package resource_test

import (
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/shell"
)

// TestTheme_AbsentCookieRendersDark verifies F1: a request with NO sb-t cookie
// renders <html class="dark"> — the load-bearing backward-compat invariant.
// Three downstream admins (go-grad, go-nerv, oxpulse-admin) pick this up on a
// version bump without asking for it; none may change appearance until an
// operator clicks the toggle.
//
// Falsification (F1): in shell.themeClass, change the default branch to return
// ThemeLight instead of ThemeDark. This test MUST go RED.
func TestTheme_AbsentCookieRendersDark(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<html lang="en" class="dark">`) {
		t.Fatalf("absent sb-t cookie must render class=\"dark\" (backward-compat); got HTML head:\n%s", headSnippet(body))
	}
	if strings.Contains(body, `<html lang="en" class="light">`) {
		t.Fatal("absent sb-t cookie must NOT render class=\"light" + "\"")
	}
}

// TestTheme_UnrecognisedCookieRendersDark verifies F2a: a garbage sb-t value
// (e.g. "purple") arriving via cookie renders dark, not light or unstyled.
//
// Falsification (F2a): in resource/render.go chromeStateFrom, change the
// validation to pass any non-empty value through (state.Theme = c.Value).
// With the pre-fix themeClass (verbatim passthrough), themeClass("purple")
// would return "purple" → class="purple" → RED. After Fix 2 (total themeClass),
// themeClass("purple") returns "dark" regardless, so this mutation alone does
// NOT make F2a go RED — themeClass is the second guard (defense-in-depth).
// F2b is the test that verifies the themeClass guard directly.
func TestTheme_UnrecognisedCookieRendersDark(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: shell.ThemeCookie, Value: "purple"})
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<html lang="en" class="dark">`) {
		t.Fatalf("unrecognised sb-t value must render class=\"dark\"; got:\n%s", headSnippet(body))
	}
}

// TestTheme_LightCookieRendersLight verifies the positive case: sb-t=light
// renders <html class="light">, exercising the full path from cookie →
// chromeStateFrom → ChromeState.Theme → themeClass → template emission.
func TestTheme_LightCookieRendersLight(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: shell.ThemeCookie, Value: shell.ThemeLight})
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<html lang="en" class="light">`) {
		t.Fatalf("sb-t=light must render class=\"light\"; got:\n%s", headSnippet(body))
	}
}

// TestTheme_DarkCookieRendersDark verifies sb-t=dark renders dark explicitly.
func TestTheme_DarkCookieRendersDark(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: shell.ThemeCookie, Value: shell.ThemeDark})
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<html lang="en" class="dark">`) {
		t.Fatalf("sb-t=dark must render class=\"dark\"; got:\n%s", headSnippet(body))
	}
}

// TestTheme_StylesheetHasBothPalettes verifies F3: the rendered stylesheet
// contains a :root.dark block (dark palette) AND a bare :root block with a
// different --bg-base value (light palette). This is the reachability check
// for the light palette — if the dark palette were still on bare :root, the
// two --bg-base values would be identical.
//
// Falsification (F3): in shell/styles.templ, change the :root.dark selector
// back to bare :root. Both :root blocks would then have the same selector;
// the last one wins, so --bg-base would be the dark value in both. The test
// checks that the bare-:root --bg-base differs from the :root.dark --bg-base,
// so it MUST go RED.
func TestTheme_StylesheetHasBothPalettes(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()

	// Must contain :root.dark selector (dark palette).
	if !strings.Contains(body, ":root.dark{") {
		t.Fatal("stylesheet must contain a :root.dark block for the dark palette")
	}

	// Must contain bare :root selector (light palette).
	if !strings.Contains(body, ":root{") {
		t.Fatal("stylesheet must contain a bare :root block for the light palette")
	}

	// The bare-:root --bg-base must differ from the :root.dark --bg-base.
	// Light --bg-base is #ffffff; dark --bg-base is #0c1222.
	lightBg := extractToken(body, ":root{", "--bg-base:")
	darkBg := extractToken(body, ":root.dark{", "--bg-base:")
	if lightBg == "" {
		t.Fatal("could not extract --bg-base from bare :root block")
	}
	if darkBg == "" {
		t.Fatal("could not extract --bg-base from :root.dark block")
	}
	if lightBg == darkBg {
		t.Fatalf("light and dark --bg-base must differ; both are %q — palette split is broken", lightBg)
	}
	if lightBg != "#ffffff" {
		t.Errorf("expected light --bg-base=#ffffff, got %q", lightBg)
	}
	if darkBg != "#0c1222" {
		t.Errorf("expected dark --bg-base=#0c1222, got %q", darkBg)
	}
}

// TestTheme_ToggleButtonPresent verifies the theme toggle button is rendered
// in the sidebar header with the required accessibility attributes, including
// aria-pressed reflecting the current theme state.
func TestTheme_ToggleButtonPresent(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="theme-toggle"`) {
		t.Fatal("theme toggle button (id=theme-toggle) must be present in the sidebar header")
	}
	if !strings.Contains(body, `class="theme-toggle"`) {
		t.Error("theme toggle button must have class=\"theme-toggle\"")
	}
	// aria-label must name the action (default dark → "Switch to light theme").
	if !strings.Contains(body, `aria-label="Switch to light theme"`) {
		t.Error("theme toggle must have an aria-label naming the action")
	}
	// aria-pressed must reflect the current state (default dark → "false").
	if !strings.Contains(body, `aria-pressed="false"`) {
		t.Error("theme toggle must have aria-pressed=\"false\" when dark is active (state exposed to screen readers)")
	}
}

// TestTheme_ToggleButtonPressedFlipsWithTheme verifies aria-pressed changes
// to "true" when the theme is light.
func TestTheme_ToggleButtonPressedFlipsWithTheme(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: shell.ThemeCookie, Value: shell.ThemeLight})
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `aria-pressed="true"`) {
		t.Error("when theme is light, aria-pressed must be \"true\"")
	}
}

// TestTheme_ToggleButtonLabelFlipsWithTheme verifies the aria-label changes
// based on the current theme: when light, it says "Switch to dark theme".
func TestTheme_ToggleButtonLabelFlipsWithTheme(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: shell.ThemeCookie, Value: shell.ThemeLight})
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `aria-label="Switch to dark theme"`) {
		t.Error("when theme is light, aria-label must say \"Switch to dark theme\"")
	}
}

// headSnippet returns the first 300 chars of body for error messages.
func headSnippet(body string) string {
	if len(body) > 300 {
		return body[:300]
	}
	return body
}

// extractToken extracts a CSS custom property value from a stylesheet string.
// blockStart is the selector (e.g. ":root{") and tokenPrefix is the property
// prefix (e.g. "--bg-base:"). Returns the value up to the next ';' or empty.
func extractToken(body, blockStart, tokenPrefix string) string {
	idx := strings.Index(body, blockStart)
	if idx < 0 {
		return ""
	}
	rest := body[idx:]
	propIdx := strings.Index(rest, tokenPrefix)
	if propIdx < 0 {
		return ""
	}
	rest = rest[propIdx+len(tokenPrefix):]
	endIdx := strings.IndexAny(rest, ";}")
	if endIdx < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:endIdx])
}

// ── WCAG contrast guard (F4) ───────────────────────────────────────────────

// TestTheme_ContrastAA verifies that every light-theme text token clears
// WCAG AA (4.5:1) against the background it actually sits on — not just
// --bg-base. Values are parsed from the rendered stylesheet, not restated
// in the test, so editing the stylesheet without meeting the threshold
// fails the test.
//
// Falsification (F4): in shell/styles.templ, set the light --text-muted
// back to #64748b. The --text-muted on --bg-elevated pair drops to 3.86:1
// < 4.5 → RED.
func TestTheme_ContrastAA(t *testing.T) {
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()

	tokens := parseLightRootTokens(t, body)

	// Resolve var() references to their parsed hex values.
	resolve := func(v string) string {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "var(") && strings.HasSuffix(v, ")") {
			ref := v[4 : len(v)-1]
			if val, ok := tokens[ref]; ok {
				return val
			}
		}
		return v
	}

	bgBase := tokens["--bg-base"]
	bgSurface := tokens["--bg-surface"]
	bgElevated := tokens["--bg-elevated"]

	type pair struct {
		fgName    string
		bgHex     string
		threshold float64
	}

	pairs := []pair{
		// The four text tokens on all three backgrounds.
		{"--text-primary", bgBase, 4.5},
		{"--text-primary", bgSurface, 4.5},
		{"--text-primary", bgElevated, 4.5},
		{"--text-secondary", bgBase, 4.5},
		{"--text-secondary", bgSurface, 4.5},
		{"--text-secondary", bgElevated, 4.5},
		{"--text-muted", bgBase, 4.5},
		{"--text-muted", bgSurface, 4.5},
		{"--text-muted", bgElevated, 4.5},
		{"--text-label", bgBase, 4.5},
		{"--text-label", bgSurface, 4.5},
		{"--text-label", bgElevated, 4.5},
		// Link accent on base and surface (the backgrounds links sit on).
		{"--accent-link", bgBase, 4.5},
		{"--accent-link", bgSurface, 4.5},
		// badge-gray-fg on elevated (the bg .badge-gray uses).
		{"--badge-gray-fg", bgElevated, 4.5},
		// fit-reject-fg on fit-reject-bg.
		{"--fit-reject-fg", resolve(tokens["--fit-reject-bg"]), 4.5},
		// fit-unscored-fg on fit-unscored-bg.
		{"--fit-unscored-fg", resolve(tokens["--fit-unscored-bg"]), 4.5},
	}

	for _, pr := range pairs {
		fgHex := resolve(tokens[pr.fgName])
		if fgHex == "" {
			t.Fatalf("could not parse %s from light :root", pr.fgName)
		}
		if pr.bgHex == "" {
			t.Fatalf("could not parse background hex for %s pair", pr.fgName)
		}
		ratio := wcagContrastRatio(fgHex, pr.bgHex)
		if ratio < pr.threshold {
			t.Errorf("%s (%s) on %s: ratio %.2f:1 < %.1f:1 (AA FAIL)",
				pr.fgName, fgHex, pr.bgHex, ratio, pr.threshold)
		}
	}
}

// parseLightRootTokens extracts all --token:value pairs from the first bare
// :root{...} block in the rendered HTML (the light palette). Stops at the
// matching closing brace so it does not bleed into :root.dark.
func parseLightRootTokens(t *testing.T, body string) map[string]string {
	t.Helper()
	idx := strings.Index(body, ":root{")
	if idx < 0 {
		t.Fatal("no bare :root{ block found in rendered HTML")
	}
	// Find the matching closing brace (accounting for nested braces —
	// the :root block has no nesting, but var() values with parens are fine
	// since we track braces, not parens).
	rest := body[idx+len(":root{"):]
	depth := 1
	end := -1
	for i, ch := range rest {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatal("unterminated :root{ block")
	}
	block := rest[:end]

	tokens := make(map[string]string)
	// Match --token:value;  (value runs to the next ; or end of block)
	re := regexp.MustCompile(`(--[a-zA-Z0-9_-]+)\s*:\s*([^;]+)`)
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		tokens[m[1]] = strings.TrimSpace(m[2])
	}
	return tokens
}

// wcagContrastRatio computes the WCAG 2.x contrast ratio between two hex
// colours. Formula: (L1+0.05)/(L2+0.05) where L1 is the lighter luminance.
func wcagContrastRatio(fgHex, bgHex string) float64 {
	fg := relativeLuminance(fgHex)
	bg := relativeLuminance(bgHex)
	lighter := math.Max(fg, bg)
	darker := math.Min(fg, bg)
	return (lighter + 0.05) / (darker + 0.05)
}

// relativeLuminance computes the WCAG relative luminance of a #rrggbb colour.
func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0
	}
	r := hexToLinear(hex[0:2])
	g := hexToLinear(hex[2:4])
	b := hexToLinear(hex[4:6])
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// hexToLinear converts a 2-digit hex channel to its linear sRGB value.
func hexToLinear(h string) float64 {
	v, err := strconv.ParseInt(h, 16, 32)
	if err != nil {
		return 0
	}
	s := float64(v) / 255.0
	if s <= 0.03928 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}
