package shell_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/shell"
)

func TestSkinRegistry_Shape(t *testing.T) {
	skins := shell.Skins()
	if len(skins) < 2 || skins[0].Name != shell.SkinGraphite {
		t.Fatalf("registry must start with graphite and hold more than one skin, got %d", len(skins))
	}
	seen := map[string]bool{}
	hex := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	for _, s := range skins {
		if seen[s.Name] {
			t.Errorf("duplicate skin %q", s.Name)
		}
		seen[s.Name] = true
		if s.Label.For("en") == "" || s.Label.For("ru") == "" || s.Label.For("ru") == s.Label.For("en") {
			t.Errorf("%s: needs distinct en and ru labels", s.Name)
		}
		if s.Label.For("de") != s.Label.For("en") {
			t.Errorf("%s: an untranslated language must fall back to English", s.Name)
		}
		if s.Name != shell.SkinGraphite && s.Head == nil {
			t.Errorf("%s: a non-graphite skin without Head renders as graphite", s.Name)
		}
		for name, c := range map[string]shell.Swatch{"light": s.Light, "dark": s.Dark} {
			for _, v := range []string{c.Ground, c.Tile, c.Ink, c.Accent} {
				if !hex.MatchString(v) {
					t.Errorf("%s %s: swatch colour %q is not #rrggbb", s.Name, name, v)
				}
			}
		}
	}
	if _, ok := shell.LookupSkin("evil"); ok {
		t.Error("LookupSkin accepted an unregistered name")
	}
	if got := shell.SkinsNamed([]string{"iris", "evil", "iris", "graphite"}); len(got) != 2 || got[0].Name != "iris" || got[1].Name != "graphite" {
		t.Errorf("SkinsNamed must keep order and drop unknown and repeated names, got %v", got)
	}
}

// tokenBlock returns the custom properties declared by the first CSS rule in
// html whose selector is exactly sel.
func tokenBlock(t *testing.T, html, sel string) map[string]string {
	t.Helper()
	i := strings.Index(html, sel+"{")
	if i < 0 {
		t.Fatalf("no %s rule in the page", sel)
	}
	body := html[i+len(sel)+1:]
	body, _, _ = strings.Cut(body, "}")
	out := map[string]string{}
	for _, decl := range strings.Split(body, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if ok && strings.HasPrefix(strings.TrimSpace(k), "--") {
			out[strings.TrimSpace(k)] = strings.ToLower(strings.TrimSpace(v))
		}
	}
	return out
}

// TestSkinRegistry_SwatchesMatchTheRealTokens: an option draws a skin other than
// the page's own, so its colours are literals in the registry. This oracle
// holds each to the token the skin's own CSS declares, in both themes.
//
// Falsification: change a swatch colour in shell/skins.go (iris Light.Accent to
// #5046e5) -> RED naming the skin, theme and role.
func TestSkinRegistry_SwatchesMatchTheRealTokens(t *testing.T) {
	for _, s := range shell.Skins() {
		page := renderLayout(t, shell.ContextWithChrome(context.Background(), shell.ChromeState{Skin: s.Name}), nil)
		lightSel, darkSel := ":root", ":root.dark"
		if s.Name != shell.SkinGraphite {
			lightSel, darkSel = ":root[data-skin="+s.Name+"]", ":root.dark[data-skin="+s.Name+"]"
		}
		light, dark := tokenBlock(t, page, lightSel), tokenBlock(t, page, darkSel)
		for theme, c := range map[string]struct {
			want shell.Swatch
			toks map[string]string
		}{"light": {s.Light, light}, "dark": {s.Dark, dark}} {
			for role, pair := range map[string][2]string{
				"ground": {c.want.Ground, c.toks["--bg-deep"]},
				"tile":   {c.want.Tile, c.toks["--bg-raised"]},
				"ink":    {c.want.Ink, c.toks["--text-primary"]},
				"accent": {c.want.Accent, c.toks["--accent"]},
			} {
				if strings.ToLower(pair[0]) != pair[1] {
					t.Errorf("%s %s %s: registry swatch %s, the skin's CSS says %q", s.Name, theme, role, pair[0], pair[1])
				}
			}
		}
	}
}

func renderRows(t *testing.T, st shell.ChromeState) string {
	t.Helper()
	ctx := shell.ContextWithChrome(context.Background(), st)
	var b bytes.Buffer
	if err := components.SettingsSectionView(components.SettingsSection{Rows: shell.ThemeRows(ctx)}).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestThemeRows_OneOptionPerRegistrySkin (F6): every registered skin is an
// option of the «Тема» row, whatever the template says.
func TestThemeRows_OneOptionPerRegistrySkin(t *testing.T) {
	html := renderRows(t, shell.ChromeState{Skin: shell.SkinIris, Skins: shell.Skins()})
	for _, s := range shell.Skins() {
		if !strings.Contains(html, `name="theme-skin" value="`+s.Name+`"`) {
			t.Errorf("no option for registry skin %q", s.Name)
		}
	}
	if n := strings.Count(html, `name="theme-skin"`); n != len(shell.Skins()) {
		t.Errorf("%d skin radios for %d registry skins", n, len(shell.Skins()))
	}
	if !regexp.MustCompile(`name="theme-skin" value="iris" checked`).MatchString(html) {
		t.Error("the rendered skin is not the checked option")
	}
}

// TestThemeRows_ListComesFromTheCaller: a skin that exists nowhere else still
// gets its option, and one the caller did not pass does not.
//
// Falsification (F6b): hard-code the options in components/theme.templ
// (range over a literal instead of c.Skins) and regenerate -> RED.
func TestThemeRows_ListComesFromTheCaller(t *testing.T) {
	probe := shell.Skin{
		Name: "probe-skin", Label: shell.Text{"en": "Probe label"}, DefaultTheme: shell.ThemeDark,
		Light: shell.Swatch{Ground: "#010203", Tile: "#040506", Ink: "#070809", Accent: "#0a0b0c"},
	}
	html := renderRows(t, shell.ChromeState{Skins: []shell.Skin{probe, shell.Skins()[0]}})
	for _, want := range []string{`value="probe-skin"`, "Probe label", "--sw-lg:#010203"} {
		if !strings.Contains(html, want) {
			t.Errorf("a skin passed by the caller lacks %q", want)
		}
	}
	if strings.Contains(html, `value="iris"`) {
		t.Error("a skin the caller did not pass was rendered")
	}
}

// TestThemeRows_NeedTwoSkinsForTheThemeRow (F6c): fewer than two allowed skins
// means no «Тема» row (nothing to choose); the mode row is always there.
func TestThemeRows_NeedTwoSkinsForTheThemeRow(t *testing.T) {
	for name, st := range map[string]shell.ChromeState{
		"none": {}, "one": {Skins: shell.Skins()[:1]},
	} {
		html := renderRows(t, st)
		if strings.Contains(html, "theme-skin") || strings.Contains(html, "How the panel looks") || strings.Count(html, `class="gp-set-row"`) != 1 {
			t.Errorf("%s allowed skin(s): the theme row (label, hint or empty control) rendered; set-rows = %d, want only the mode row", name, strings.Count(html, `class="gp-set-row"`))
		}
		if !strings.Contains(html, `name="theme-mode"`) {
			t.Errorf("%s: the mode row is missing", name)
		}
	}
}

func TestThemeSwatchStyleCannotCarryADeclaration(t *testing.T) {
	bad := shell.Skin{Name: "bad", Label: shell.Text{"en": "Bad"}, Light: shell.Swatch{Ground: `red;background:url(//evil.example/x)`, Tile: "#ffffff"}}
	html := renderRows(t, shell.ChromeState{Skins: []shell.Skin{bad, shell.Skins()[0]}})
	if strings.Contains(html, "evil.example") {
		t.Error("a malformed swatch colour reached the style attribute")
	}
	if !strings.Contains(html, "--sw-lt:#ffffff") {
		t.Error("a valid swatch colour was dropped")
	}
}

func TestSidebar_SettingsLinkOnlyWhenMounted(t *testing.T) {
	link := `href="/admin/settings/"`
	on := shell.ChromeState{SettingsLink: "/admin/settings/"}
	if !strings.Contains(renderLayout(t, shell.ContextWithChrome(context.Background(), on), nil), link) {
		t.Error("single-user sidebar lacks the Settings link")
	}
	if strings.Contains(renderLayout(t, shell.ContextWithChrome(context.Background(), shell.ChromeState{}), nil), "settings") {
		t.Error("Settings link rendered without MountSettings")
	}
}
