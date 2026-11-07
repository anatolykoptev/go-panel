package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

func skinPanel(skin, defTheme string) *resource.Panel {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin", Password: "secret", HMACKey: []byte("test-hmac-key-32-bytes-long-here"),
		BasePath: "/admin",
	})
	return resource.New(resource.Config{Title: "Skin Panel", BasePath: "/admin", Auth: a, Skin: skin, DefaultTheme: defTheme})
}

func renderSkinned(t *testing.T, p *resource.Panel, cookies ...*http.Cookie) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/x", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	return rec.Body.String()
}

func getLogin(t *testing.T, p *resource.Panel, cookies ...*http.Cookie) string {
	t.Helper()
	return getLoginAt(t, p, "/admin/login", cookies...)
}

func getLoginAt(t *testing.T, p *resource.Panel, path string, cookies ...*http.Cookie) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status %d", rec.Code)
	}
	return rec.Body.String()
}

// TestSkin_PanelWithoutSkinIsUnchanged: Config{} and an unrecognised Skin render
// identical bytes (no data-skin, no fonts), on the layout AND the login page.
//
// Falsification (F1): make shell resolveSkin return iris for "" -> RED.
func TestSkin_PanelWithoutSkinIsUnchanged(t *testing.T) {
	base, junk := skinPanel("", ""), skinPanel("neon", "")
	if a, b := renderSkinned(t, base), renderSkinned(t, junk); a != b {
		t.Error("an unrecognised Skin changed the layout bytes")
	}
	if a, b := getLogin(t, base), getLogin(t, junk); a != b {
		t.Error("an unrecognised Skin changed the login bytes")
	}
	for _, html := range []string{renderSkinned(t, base), getLogin(t, base)} {
		for _, leak := range []string{"data-skin", "woff2", "Manrope"} {
			if strings.Contains(html, leak) {
				t.Errorf("unskinned panel output contains %q", leak)
			}
		}
		if !strings.Contains(html, `class="dark"`) {
			t.Error("unskinned panel must stay dark")
		}
	}
}

// TestSkin_PanelIrisDefaultsLightAndHonoursCookie: the manager's first visit
// (no cookie) is light; the cookie, in either direction, wins; DefaultTheme
// overrides the skin default.
//
// Falsification (F2): in shell resolveTheme return ThemeDark instead of the
// skin default -> RED on the first assertion.
func TestSkin_PanelIrisDefaultsLightAndHonoursCookie(t *testing.T) {
	p := skinPanel(shell.SkinIris, "")
	html := renderSkinned(t, p)
	if !strings.Contains(html, `class="light" data-skin="iris"`) {
		t.Errorf("iris first visit must render light + data-skin; head: %.160s", html)
	}
	if h := renderSkinned(t, p, &http.Cookie{Name: shell.ThemeCookie, Value: "dark"}); !strings.Contains(h, `class="dark" data-skin="iris"`) {
		t.Error("sb-t=dark must win over the skin default")
	}
	if h := renderSkinned(t, p, &http.Cookie{Name: shell.ThemeCookie, Value: "garbage"}); !strings.Contains(h, `class="light" data-skin="iris"`) {
		t.Error("a garbage cookie must fall back to the skin default")
	}
	if h := renderSkinned(t, skinPanel(shell.SkinIris, shell.ThemeDark)); !strings.Contains(h, `class="dark" data-skin="iris"`) {
		t.Error("DefaultTheme=dark must override the iris default")
	}
	if h := renderSkinned(t, skinPanel("", shell.ThemeLight)); !strings.Contains(h, `class="light"`) || strings.Contains(h, "data-skin") {
		t.Error("DefaultTheme works without a skin and must not emit data-skin")
	}
}

// TestSkin_PanelLoginIsSkinned: the default login page the authenticator mounts
// on the mux is skinned, follows the theme cookie, and stays dark-graphite for
// an unskinned panel.
//
// Falsification (F4): drop the data-skin attribute in LoginPage, or the
// withSkin middleware in resource.Handler -> RED.
func TestSkin_PanelLoginIsSkinned(t *testing.T) {
	p := skinPanel(shell.SkinIris, "")
	html := getLogin(t, p)
	if !strings.Contains(html, `class="light" data-skin="iris"`) || !strings.Contains(html, "/admin/static/fonts/inter-latin-wght-normal.woff2") {
		t.Errorf("login of an iris panel is not skinned; head: %.200s", html)
	}
	if h := getLogin(t, p, &http.Cookie{Name: shell.ThemeCookie, Value: "dark"}); !strings.Contains(h, `class="dark" data-skin="iris"`) {
		t.Error("login must follow the theme cookie on a skinned panel")
	}
}

// TestSkin_RenderErrorStaysSkinned: RenderError has no request, so the panel
// fills its own skin into a context that names none.
func TestSkin_RenderErrorStaysSkinned(t *testing.T) {
	p := skinPanel(shell.SkinIris, "")
	rec := httptest.NewRecorder()
	p.RenderError(context.Background(), rec, "T", nil, templ.Raw("<p>x</p>"), "x", "boom")
	if !strings.Contains(rec.Body.String(), `data-skin="iris"`) {
		t.Error("RenderError on an iris panel rendered graphite")
	}
	rec = httptest.NewRecorder()
	skinPanel("", "").RenderError(context.Background(), rec, "T", nil, templ.Raw("<p>x</p>"), "x", "boom")
	if strings.Contains(rec.Body.String(), "data-skin") {
		t.Error("RenderError on a graphite panel leaked the skin")
	}
}

// TestSkin_FontsServedFromThePanel: the woff2 files the skin references are
// really served (200, font type, wOF2 magic) from the panel's static mount, and
// the OFL licences ship beside them.
func TestSkin_FontsServedFromThePanel(t *testing.T) {
	h := skinPanel(shell.SkinIris, "").Handler()
	for _, f := range []string{"inter-latin", "inter-cyrillic", "manrope-latin", "manrope-cyrillic"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/static/fonts/"+f+"-wght-normal.woff2", nil))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "wOF2") {
			t.Errorf("%s: status %d, magic %q", f, rec.Code, rec.Body.String()[:min(4, rec.Body.Len())])
		}
	}
	// The OFL licences travel inside the binary: they are served from the same
	// embedded FS as the fonts (not read from disk), the README is not.
	for _, f := range []string{"LICENSE-Inter-OFL.txt", "LICENSE-Manrope-OFL.txt"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/static/fonts/"+f, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SIL OPEN FONT LICENSE") {
			t.Errorf("%s not served from the embedded FS (status %d)", f, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/static/fonts/README.md", nil))
	if rec.Code == http.StatusOK {
		t.Error("fonts/README.md must not be embedded or served")
	}
}

// TestSkin_FontURLsFollowThePanelBasePath: Layout's skin font URLs use the
// panel's mount, like login/MFA do (the pm7.css link keeps its own hardcode).
func TestSkin_FontURLsFollowThePanelBasePath(t *testing.T) {
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "a", Password: "b", HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/ops"})
	p := resource.New(resource.Config{Title: "T", BasePath: "/ops", Auth: a, Skin: shell.SkinIris})
	html := renderSkinned(t, p)
	if !strings.Contains(html, "url(/ops/static/fonts/inter-latin-wght-normal.woff2)") || strings.Contains(html, "url(/admin/static/fonts/") {
		t.Error("layout font URLs do not follow BasePath")
	}
	if l := getLoginAt(t, p, "/ops/login"); !strings.Contains(l, "url(/ops/static/fonts/") {
		t.Error("login font URLs do not follow BasePath")
	}
}

// ── Token tables, parsed from the stylesheet the panel actually serves ─────

func irisRender(t *testing.T) string {
	t.Helper()
	return renderSkinned(t, skinPanel(shell.SkinIris, ""))
}

// irisThemes returns the effective custom properties of iris light and dark,
// following the real cascade: graphite light (:root) < graphite dark
// (:root.dark, dark only) < iris light (:root[data-skin=iris], both) < iris
// dark (:root.dark[data-skin=iris], dark only).
func irisThemes(t *testing.T) (light, dark map[string]string) {
	t.Helper()
	body := irisRender(t)
	gLight := blockTokens(t, body, ":root{")
	gDark := blockTokens(t, body, ":root.dark{")
	iLight := blockTokens(t, body, ":root[data-skin=iris]{")
	iDark := blockTokens(t, body, ":root.dark[data-skin=iris]{")
	light, dark = map[string]string{}, map[string]string{}
	for _, m := range []map[string]string{gLight, iLight} {
		for k, v := range m {
			light[k] = v
		}
	}
	for _, m := range []map[string]string{gLight, gDark, iLight, iDark} {
		for k, v := range m {
			dark[k] = v
		}
	}
	return light, dark
}

var (
	hexColour  = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	baseBlue   = regexp.MustCompile(`rgba?\(\s*0[ ,]+(95|120)[ ,]+(184|212)`)
	numSuffix  = regexp.MustCompile(`-\d+$`)
	graphiteBg = regexp.MustCompile(`#(0078d4|005fb8|4daafc|141414|181818|1f1f1f|282828|f3f3f3|f8f8f8)\b`)
)

func isColourToken(v string) bool { return hexColour.MatchString(v) || baseBlue.MatchString(v) }

// TestSkinIris_TokenSetIsComplete: no graphite colour can leak through. Every
// colour token graphite defines (light block, and dark block for dark) is also
// defined by the matching iris block — derived from the stylesheet, so a token
// added to graphite tomorrow is caught — and no iris value is a graphite blue
// or graphite ground.
func TestSkinIris_TokenSetIsComplete(t *testing.T) {
	body := irisRender(t)
	gLight, gDark := blockTokens(t, body, ":root{"), blockTokens(t, body, ":root.dark{")
	iLight, iDark := blockTokens(t, body, ":root[data-skin=iris]{"), blockTokens(t, body, ":root.dark[data-skin=iris]{")
	var missing []string
	for k, v := range gLight {
		if isColourToken(v) {
			if _, ok := iLight[k]; !ok {
				missing = append(missing, "light "+k)
			}
		}
	}
	for k, v := range gDark {
		if isColourToken(v) {
			if _, ok := iDark[k]; !ok {
				if _, ok := iLight[k]; !ok {
					missing = append(missing, "dark "+k)
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("iris leaves graphite colour tokens unset: %v", missing)
	}
	for _, m := range []map[string]string{iLight, iDark} {
		for k, v := range m {
			if graphiteBg.MatchString(strings.ToLower(v)) || baseBlue.MatchString(v) {
				t.Errorf("iris token %s = %s is a graphite colour", k, v)
			}
		}
	}
	if len(iLight) < 60 || len(iDark) < 40 {
		t.Errorf("iris token blocks parsed too small: light %d dark %d", len(iLight), len(iDark))
	}
}

type irisPair struct {
	fg, bg, why string
	min         float64
}

// irisPairs derives every pair to check from the token set itself, by naming
// convention, so a new token cannot escape:
//   - text tokens (--text-*, minus --text-faint, and --accent-text) on every
//     ground and on --accent-soft, >= 4.5;
//   - a status role (--X with --X-bg) on its own -bg and on the grounds, >= 4.5;
//   - --on-X on --X, --X-bg, --X-hover (trailing -N stripped), >= 4.5;
//   - --btn-*-bg and -hover under white (or --on-btn-<name>), >= 4.5;
//   - UI: --border-control, --accent on every ground, --*-signal on the tile,
//     >= 3; --track on the tile >= 1.5.
func irisPairs(tokens map[string]string) []irisPair {
	grounds := []string{"--bg-deep", "--bg-base", "--bg-raised", "--bg-elevated"}
	var ps []irisPair
	var names []string
	for k := range tokens {
		names = append(names, k)
	}
	sort.Strings(names)
	has := func(n string) bool { _, ok := tokens[n]; return ok }
	for _, k := range names {
		switch {
		case (strings.HasPrefix(k, "--text-") && k != "--text-faint") || k == "--accent-text":
			for _, g := range append(append([]string{}, grounds...), "--accent-soft") {
				ps = append(ps, irisPair{k, g, "text", 4.5})
			}
		case strings.HasPrefix(k, "--on-"):
			x := "--" + numSuffix.ReplaceAllString(strings.TrimPrefix(k, "--on-"), "")
			for _, s := range []string{x, x + "-bg", x + "-hover"} {
				if has(s) {
					ps = append(ps, irisPair{k, s, "on-surface", 4.5})
				}
			}
		case strings.HasSuffix(k, "-bg") && has(strings.TrimSuffix(k, "-bg")) && !strings.HasPrefix(k, "--bg-") && !strings.HasPrefix(k, "--btn-") && !strings.HasPrefix(k, "--pm7-"):
			fg := strings.TrimSuffix(k, "-bg")
			ps = append(ps, irisPair{fg, k, "status", 4.5}, irisPair{fg, "--bg-raised", "status", 4.5}, irisPair{fg, "--bg-base", "status", 4.5})
		case strings.HasPrefix(k, "--btn-") && (strings.HasSuffix(k, "-bg") || strings.HasSuffix(k, "-hover")):
			name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(k, "--btn-"), "-bg"), "-hover")
			fg := "#ffffff"
			if has("--on-btn-" + name) {
				fg = "--on-btn-" + name
			}
			ps = append(ps, irisPair{fg, k, "button", 4.5})
		case strings.HasSuffix(k, "-signal"):
			ps = append(ps, irisPair{k, "--bg-raised", "ui", 3})
		}
	}
	for _, g := range grounds {
		ps = append(ps, irisPair{"--border-control", g, "ui", 3}, irisPair{"--accent", g, "ui", 3})
	}
	ps = append(ps, irisPair{"--track", "--bg-raised", "track", 1.5})
	return ps
}

// TestSkinIris_ContrastAA asserts WCAG AA for every derived pair in BOTH
// renditions: text pairs >= 4.5, UI components (control border, focus ring,
// status dots) >= 3.
//
// Falsification (F3): set the iris LIGHT --text-muted to #9AA0B8 in
// shell/skin_iris.templ -> muted on canvas/tile/soft drops under 4.5 -> RED.
func TestSkinIris_ContrastAA(t *testing.T) {
	light, dark := irisThemes(t)
	total := 0
	for name, tokens := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, p := range irisPairs(tokens) {
			fg, bg := mustHex(t, "iris "+name, tokens, tokRef(p.fg)), mustHex(t, "iris "+name, tokens, tokRef(p.bg))
			if fg == "" || bg == "" {
				continue
			}
			total++
			if r := wcagContrastRatio(fg, bg); r < p.min {
				t.Errorf("iris %s: %s (%s) on %s (%s) = %.2f:1 < %.1f (%s)", name, p.fg, fg, p.bg, bg, r, p.min, p.why)
			}
		}
	}
	if total < 100 {
		t.Fatalf("only %d iris pairs derived; the derivation no longer sees the token set", total)
	}
	// the derivation must reach the new roles, not just the legacy ones.
	need := map[string]bool{"--on-night": false, "--on-btn-primary": false, "--special-signal": false, "--text-muted": false}
	for _, p := range irisPairs(light) {
		for _, s := range []string{p.fg, p.bg} {
			if _, ok := need[s]; ok {
				need[s] = true
			}
		}
	}
	for k, ok := range need {
		if !ok {
			t.Errorf("derivation never checks %s", k)
		}
	}
}

var irisSel = regexp.MustCompile(`^:root\[data-skin=iris\]`)

// TestSkinIris_RuleDerivedPairsAA: every iris rule that sets a background and a
// colour (tokens) clears 4.5:1 in both renditions; and the iris stylesheet
// uses no undefined var() and no font-size under 12px.
func TestSkinIris_RuleDerivedPairsAA(t *testing.T) {
	body := irisRender(t)
	light, dark := irisThemes(t)
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(body, "")
	checked := 0
	fs := regexp.MustCompile(`font-size\s*:\s*([0-9.]+)rem`)
	for _, m := range ruleRe.FindAllStringSubmatch(css, -1) {
		sel := strings.TrimSpace(m[1])
		if !irisSel.MatchString(sel) || strings.HasSuffix(sel, "{") {
			continue
		}
		for _, f := range fs.FindAllStringSubmatch(m[2], -1) {
			if v := mustFloat(f[1]); v < 0.75 {
				t.Errorf("iris rule %q: font-size %srem < 12px", sel, f[1])
			}
		}
		for _, u := range varUse.FindAllStringSubmatch(m[2], -1) {
			if _, ok := light[u[1]]; !ok && !strings.HasPrefix(u[1], "--tone-") && !strings.HasPrefix(u[1], "--pm7-") {
				t.Errorf("iris rule %q uses undefined token %s", sel, u[1])
			}
		}
		bgv := decl(m[2], "background")
		if bgv == "" {
			bgv = decl(m[2], "background-color")
		}
		fgv := strings.TrimSuffix(decl(m[2], "color"), "!important")
		bgv = strings.TrimSuffix(bgv, "!important")
		if bgv == "" || fgv == "" {
			continue
		}
		for name, tokens := range map[string]map[string]string{"light": light, "dark": dark} {
			fg, ok1 := hex6(resolveColor(tokens, fgv))
			bg, ok2 := hex6(resolveColor(tokens, bgv))
			if !ok1 || !ok2 {
				continue
			}
			checked++
			if r := wcagContrastRatio(fg, bg); r < 4.5 {
				t.Errorf("iris %s: rule %q: color %s on %s = %.2f:1 < 4.5", name, sel, fgv, bgv, r)
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d iris rule pairs checked", checked)
	}
}

func mustFloat(s string) float64 {
	var f float64
	for i, part := range strings.SplitN(s, ".", 2) {
		var n float64
		scale := 1.0
		for _, c := range part {
			n = n*10 + float64(c-'0')
			if i == 1 {
				scale *= 10
			}
		}
		if i == 0 {
			f = n
		} else {
			f += n / scale
		}
	}
	return f
}

// TestSkinIris_RowActionsAreSoftNotInk: under iris the ink fill is reserved for
// the one page-level CTA. A work-queue row (or list row) action, even when the
// component marks it primary, must paint the soft pair, never --btn-primary-bg.
//
// Falsification: delete the `.queue-actions .btn-primary,.crm-table .btn-primary`
// rule in shell/skin_iris.templ (the line right above the .btn-secondary rule)
// -> no soft rule for the primary row action -> RED.
func TestSkinIris_RowActionsAreSoftNotInk(t *testing.T) {
	var row strings.Builder
	qr := components.QueueRow{ID: "q", Title: "T", Actions: []components.RowAction{{Label: "Take", Variant: components.VariantPrimary, Href: "/x"}}}
	if err := components.QueueRowView(qr).Render(context.Background(), &row); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(row.String(), `class="btn btn-primary`) || !strings.Contains(row.String(), "queue-actions") {
		t.Fatalf("queue row markup changed; the skin selector no longer targets it: %s", row.String())
	}
	body := irisRender(t)
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(body, "")
	for _, anchor := range []string{".queue-actions .btn-primary", ".crm-table .btn-primary"} {
		found := false
		for _, m := range ruleRe.FindAllStringSubmatch(css, -1) {
			sel := strings.TrimSpace(m[1])
			if !strings.Contains(sel, ":root[data-skin=iris] "+anchor) || strings.Contains(sel, ":hover") {
				continue
			}
			found = true
			if bg := decl(m[2], "background"); bg != "var(--accent-soft)" {
				t.Errorf("%s: row action background = %q, want var(--accent-soft) (not the ink fill)", anchor, bg)
			}
			if fg := decl(m[2], "color"); fg != "var(--accent-text)" {
				t.Errorf("%s: row action colour = %q, want var(--accent-text)", anchor, fg)
			}
		}
		if !found {
			t.Errorf("iris has no soft override for %s: primary row actions would render ink", anchor)
		}
	}
}

var (
	inlineMono = regexp.MustCompile(`(?s)<([a-zA-Z0-9]+)\b[^<>]*?style="[^"]*font-family:var\(--font-mono\)`)
	codeTags   = map[string]bool{"pre": true, "code": true, "kbd": true, "samp": true, "textarea": true}
)

// TestSkinIris_NoMonoSurfaceOutsideCode: iris is Inter + Manrope. Every surface
// that asks for var(--font-mono) — the stylesheet rules AND the inline styles
// in the templates, both discovered from the source, not listed here — must
// resolve to the sans stack under iris, except real code surfaces (pre, code,
// kbd, samp, textarea), which must be explicitly restored to a true mono.
//
// Falsification: delete `--font-mono:var(--font-sans);` from the iris light
// block (shell/skin_iris.templ, the line above --font-code) -> every mono
// surface (.delta, .occ-head, the filter captions in list.templ:214/228, ...)
// resolves to ui-monospace under iris -> RED. Delete the
// `textarea[style*="--font-mono"]` rule -> the JSON textarea
// (form.templ:283) renders sans -> RED.
func TestSkinIris_NoMonoSurfaceOutsideCode(t *testing.T) {
	light, dark := irisThemes(t)
	isMono := func(v string) bool { return strings.Contains(strings.ToLower(v), "mono") }
	for name, tokens := range map[string]map[string]string{"light": light, "dark": dark} {
		if v := resolveColor(tokens, "var(--font-mono)"); v == "" || isMono(v) {
			t.Errorf("iris %s: --font-mono resolves to %q, want the sans stack", name, v)
		}
		if v := resolveColor(tokens, "var(--font-code)"); !isMono(v) {
			t.Errorf("iris %s: --font-code = %q is not a monospace stack", name, v)
		}
	}
	// surfaces from the stylesheet (graphite rules that ask for the token).
	nSheet := 0
	for _, r := range styleRules(t) {
		if strings.Contains(decl(r.body, "font-family"), "var(--font-mono)") {
			nSheet++
		}
	}
	// surfaces from template sources.
	files, _ := filepath.Glob("*.templ")
	more, _ := filepath.Glob("../components/*.templ")
	var codeSurfaces, nInline int
	for _, f := range append(files, more...) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range inlineMono.FindAllStringSubmatch(string(b), -1) {
			nInline++
			if codeTags[strings.ToLower(m[1])] {
				codeSurfaces++
			}
		}
	}
	if nSheet < 20 || nInline < 5 || codeSurfaces < 1 {
		t.Fatalf("discovery found sheet=%d inline=%d code=%d mono surfaces; the scan no longer sees the sources", nSheet, nInline, codeSurfaces)
	}
	// every code surface found inline is restored by an iris rule that can reach it.
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(irisRender(t), "")
	restored := false
	for _, m := range ruleRe.FindAllStringSubmatch(css, -1) {
		if strings.Contains(m[1], `[style*="--font-mono"]`) && strings.Contains(m[1], "textarea") &&
			decl(m[2], "font-family") == "var(--font-code)!important" {
			restored = true
		}
	}
	if !restored {
		t.Error("an inline-mono textarea exists but iris has no !important font-code rule for textarea[style*=\"--font-mono\"]")
	}
	// iris itself never sets a monospace family outside the code surfaces.
	for _, m := range ruleRe.FindAllStringSubmatch(css, -1) {
		sel := strings.TrimSpace(m[1])
		if !irisSel.MatchString(sel) {
			continue
		}
		ff := decl(m[2], "font-family")
		if ff == "" || (!isMono(ff) && !strings.Contains(ff, "--font-mono")) {
			continue
		}
		for _, one := range strings.Split(sel, ",") {
			one = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(one), ":root[data-skin=iris]"))
			base := strings.Fields(one)
			if len(base) == 0 || (!codeTags[base[0]] && !strings.HasPrefix(base[0], "textarea")) {
				t.Errorf("iris rule %q sets a monospace family on a non-code surface", sel)
			}
		}
	}
}
