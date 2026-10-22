package resource_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// blockTokens parses the --token:value pairs of the first block opened by
// selector (":root{" light, ":root.dark{" dark) in the rendered stylesheet.
func blockTokens(t *testing.T, body, selector string) map[string]string {
	t.Helper()
	i := strings.Index(body, selector)
	if i < 0 {
		t.Fatalf("no %q block in rendered page", selector)
	}
	rest := body[i+len(selector):]
	end := strings.Index(rest, "\n\t\t}")
	if end < 0 {
		t.Fatalf("unterminated %q block", selector)
	}
	out := map[string]string{}
	re := regexp.MustCompile(`(--[a-zA-Z0-9_-]+)\s*:\s*([^;]+)`)
	for _, m := range re.FindAllStringSubmatch(rest[:end], -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func renderedStyles(t *testing.T) string {
	t.Helper()
	p := newTestPanel()
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	rec := httptest.NewRecorder()
	if err := p.RenderPage(rec, req, "T", "", templ.Raw("")); err != nil {
		t.Fatal(err)
	}
	return rec.Body.String()
}

// TestTheme_RedesignPairsAA: every new text/fill pair the redesign components
// and button system use clears 4.5:1 in BOTH themes. Values come from the
// rendered stylesheet; a pair is (fg token, bg token).
//
// Falsification: set the light --orange-light to #f97316 (the raw --orange) ->
// the orange chip pair drops to ~2.6:1 -> RED. Set --btn-primary-bg to
// #3b82f6 -> white-on-accent 3.68:1 -> RED.
func TestTheme_RedesignPairsAA(t *testing.T) {
	body := renderedStyles(t)
	themes := map[string]map[string]string{
		"light": blockTokens(t, body, ":root{"),
		"dark":  blockTokens(t, body, ":root.dark{"),
	}
	// light fills inherit nothing from dark; dark block omits tokens it does not
	// redefine, so resolve through the light block as the fallback.
	light := themes["light"]
	resolve := func(theme map[string]string, v string) string {
		for i := 0; i < 4; i++ {
			v = strings.TrimSpace(v)
			if !strings.HasPrefix(v, "var(") {
				break
			}
			ref := v[4 : len(v)-1]
			nv, ok := theme[ref]
			if !ok {
				nv = light[ref]
			}
			v = nv
		}
		return v
	}
	type pair struct{ name, fg, bg string }
	pairs := []pair{
		{"chip orange", "--orange-light", "--orange-dim"},
		{"chip purple", "--purple-light", "--purple-dim"},
		{"chip yellow", "--yellow-light", "--yellow-dim"},
		{"chip blue", "--accent-light", "--accent-dim"},
		{"chip green", "--green-light", "--green-dim"},
		{"chip red", "--red-light", "--red-dim"},
		{"chip gray", "--badge-gray-fg", "--bg-elevated"},
		{"btn primary", "#ffffff", "--btn-primary-bg"},
		{"btn primary hover", "#ffffff", "--btn-primary-hover"},
		{"btn danger", "#ffffff", "--btn-danger-bg"},
		{"btn danger hover", "#ffffff", "--btn-danger-hover"},
		{"btn ghost / link on surface", "--accent-link", "--bg-surface"},
		{"btn secondary text", "--text-primary", "--bg-base"},
		{"queue meta / secondary", "--text-secondary", "--bg-surface"},
		{"dl-compact label", "--text-secondary", "--bg-surface"},
		{"kpi delta up", "--green-light", "--bg-surface"},
		{"kpi delta down", "--red-light", "--bg-surface"},
		{"kpi delta new", "--accent-light", "--bg-surface"},
		{"form control text", "--text-primary", "--bg-deep"},
		{"form control placeholder", "--text-secondary", "--bg-deep"},
		{"next step", "--accent-link", "--bg-base"},
	}
	for themeName, tokens := range themes {
		for _, pr := range pairs {
			fg, bg := pr.fg, pr.bg
			if !strings.HasPrefix(fg, "#") {
				fg = resolve(tokens, tokens[fg])
				if fg == "" {
					fg = resolve(tokens, light[pr.fg])
				}
			}
			bgv := resolve(tokens, tokens[bg])
			if bgv == "" {
				bgv = resolve(tokens, light[bg])
			}
			if fg == "" || bgv == "" {
				t.Errorf("%s/%s: unresolved fg %q or bg %q", themeName, pr.name, pr.fg, pr.bg)
				continue
			}
			r := wcagContrastRatio(fg, bgv)
			t.Logf("%-5s %-28s %s on %s = %.2f:1", themeName, pr.name, fg, bgv, r)
			if r < 4.5 {
				t.Errorf("%s/%s: %s on %s = %.2f:1 < 4.5", themeName, pr.name, fg, bgv, r)
			}
		}
	}
}

// TestTheme_FormControlsHaveExplicitFillBothThemes guards the white-on-white
// bug: pm7.css paints .pm7-input with --pm7-input, the light block set it to
// #fff and the dark block did not override it, so dark-theme controls showed
// pale text on a white fill. Two independent guards: the dark block defines
// --pm7-input to something that is not white, and a rule pins the control's
// fill to a go-panel token.
//
// Falsification: delete `--pm7-input:var(--bg-deep);` from :root.dark -> first
// assertion RED. Delete the `input.pm7-input,select.pm7-input,...` rule -> second RED.
func TestTheme_FormControlsHaveExplicitFillBothThemes(t *testing.T) {
	body := renderedStyles(t)
	dark := blockTokens(t, body, ":root.dark{")
	v, ok := dark["--pm7-input"]
	if !ok {
		t.Fatal(":root.dark does not define --pm7-input; it inherits the light #fff")
	}
	if strings.EqualFold(v, "#fff") || strings.EqualFold(v, "#ffffff") {
		t.Fatalf("dark --pm7-input = %s (white)", v)
	}
	if !strings.Contains(body, "input.pm7-input,select.pm7-input,textarea.pm7-input{background-color:var(--pm7-input);color:var(--text-primary)") {
		t.Error("no rule pinning input/select/textarea fill + text to go-panel tokens")
	}
	if !strings.Contains(body, ":root.dark{color-scheme:dark}") || !strings.Contains(body, ":root{color-scheme:light}") {
		t.Error("color-scheme not set per theme: native select popup / datetime icon follow the OS, not the cookie")
	}
}

// TestTheme_NewTokensInBothBlocks: a token that resolves to nothing in a theme
// is invisible chip text. A token declared once as an alias (e.g.
// --orange-light:var(--special)) resolves per theme, so it must resolve to an
// opaque colour in BOTH.
func TestTheme_NewTokensInBothBlocks(t *testing.T) {
	body := renderedStyles(t)
	light, dark := blockTokens(t, body, ":root{"), blockTokens(t, body, ":root.dark{")
	for name, block := range map[string]map[string]string{"light": light, "dark": dark} {
		merged := map[string]string{}
		for k, v := range light {
			merged[k] = v
		}
		for k, v := range block {
			merged[k] = v
		}
		for _, tok := range []string{"--orange-light", "--purple-light", "--yellow-light",
			"--btn-primary-bg", "--btn-primary-hover", "--btn-danger-bg", "--btn-danger-hover"} {
			if _, ok := hex6(resolveColor(merged, merged[tok])); !ok {
				t.Errorf("%s: %s does not resolve to an opaque colour (%q)", name, tok, merged[tok])
			}
		}
	}
	for _, class := range []string{".badge-orange,", ".badge-purple,", ".btn-primary{", ".btn-secondary{", ".btn-ghost{", ".btn-danger{", ".dl-compact{"} {
		if !strings.Contains(body, class) {
			t.Errorf("stylesheet lacks %s", class)
		}
	}
}
