package resource_test

import (
	"net/http"
	"net/http/httptest"
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

// TestTheme_UnrecognisedCookieRendersDark verifies F2: a garbage sb-t value
// (e.g. "purple") renders dark, not light or unstyled.
//
// Falsification (F2): in resource/render.go chromeStateFrom, change the
// validation to pass any non-empty value through (state.Theme = c.Value).
// Then themeClass("purple") returns "dark" (since "purple" != "light"), so
// F2 would still pass. To make F2 go RED, the mutation must change themeClass
// to return the input verbatim for any non-empty value. That mutation makes
// themeClass("purple") return "purple", and the test checks for class="dark".
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
// in the sidebar header with the required accessibility attributes.
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
