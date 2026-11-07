package shell_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/shell"
)

func renderLogin(t *testing.T, ctx context.Context) string {
	t.Helper()
	var b bytes.Buffer
	ident := shell.LoginIdentifier{Label: "Username", Name: "username", Type: "text", Autocomplete: "username"}
	if err := shell.LoginPage("/admin", ident, "bad creds", []shell.LoginLink{{Label: "Help", URL: "/help"}}).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func renderMFA(t *testing.T, ctx context.Context) string {
	t.Helper()
	var b bytes.Buffer
	if err := shell.MFAPage("/admin", "bad code").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSkin_UnskinnedIsByteIdentical is the backward-compat invariant: a panel
// that never opts into a skin — zero ChromeState, the graphite name, or a skin
// value this build does not know — renders the exact bytes the pre-skin build
// did, for the layout, the login page and the MFA page. The goldens were
// captured from origin/main before the skin existed.
//
// Falsification (F1): in shell/chrome_state.go resolveSkin, make the zero
// value resolve to iris (`if skin == SkinIris || skin == ""`) -> every
// subtest below goes RED.
func TestSkin_UnskinnedIsByteIdentical(t *testing.T) {
	states := map[string]shell.ChromeState{
		"zero":     {},
		"graphite": {Skin: shell.SkinGraphite},
		"unknown":  {Skin: "neon"},
		"theme":    {Skin: "neon", Theme: "purple"},
	}
	for name, st := range states {
		ctx := shell.ContextWithChrome(context.Background(), st)
		if got := renderLayout(t, ctx, nil); got != golden(t, "chrome_zero_golden.html") {
			t.Errorf("%s: Layout differs from the pre-skin golden (skin leaked into an unskinned panel)", name)
		}
		if got := renderLogin(t, ctx); got != golden(t, "login_zero_golden.html") {
			t.Errorf("%s: LoginPage differs from the pre-skin golden", name)
		}
		if got := renderMFA(t, ctx); got != golden(t, "mfa_zero_golden.html") {
			t.Errorf("%s: MFAPage differs from the pre-skin golden", name)
		}
	}
	for _, leak := range []string{"data-skin", "woff2", "@font-face", "Manrope"} {
		if strings.Contains(golden(t, "chrome_zero_golden.html"), leak) {
			t.Errorf("golden itself contains %q", leak)
		}
	}
}

// TestSkin_ResolutionIsTotal: every input resolves to a known skin and a
// light/dark class; nothing can emit a third value.
func TestSkin_ResolutionIsTotal(t *testing.T) {
	cases := []struct {
		name         string
		st           shell.ChromeState
		skin, theme  string
		attr, authTh string
	}{
		{"zero", shell.ChromeState{}, "graphite", "dark", "", "dark"},
		{"graphite light cookie", shell.ChromeState{Theme: "light"}, "graphite", "light", "", "dark"},
		{"unknown skin", shell.ChromeState{Skin: "x", Theme: "light"}, "graphite", "light", "", "dark"},
		{"iris no cookie", shell.ChromeState{Skin: "iris"}, "iris", "light", "iris", "light"},
		{"iris dark cookie", shell.ChromeState{Skin: "iris", Theme: "dark"}, "iris", "dark", "iris", "dark"},
		{"iris junk cookie", shell.ChromeState{Skin: "iris", Theme: "purple"}, "iris", "light", "iris", "light"},
		{"iris default dark", shell.ChromeState{Skin: "iris", DefaultTheme: "dark"}, "iris", "dark", "iris", "dark"},
		{"cookie beats default", shell.ChromeState{Skin: "iris", Theme: "light", DefaultTheme: "dark"}, "iris", "light", "iris", "light"},
		{"graphite default light", shell.ChromeState{DefaultTheme: "light"}, "graphite", "light", "", "dark"},
		{"junk default", shell.ChromeState{Skin: "iris", DefaultTheme: "mauve"}, "iris", "light", "iris", "light"},
	}
	for _, c := range cases {
		if got := c.st.SkinName(); got != c.skin {
			t.Errorf("%s: SkinName = %q, want %q", c.name, got, c.skin)
		}
		if got := c.st.ThemeClass(); got != c.theme {
			t.Errorf("%s: ThemeClass = %q, want %q", c.name, got, c.theme)
		}
		if got := c.st.SkinAttr(); got != c.attr {
			t.Errorf("%s: SkinAttr = %q, want %q", c.name, got, c.attr)
		}
		if got := c.st.AuthThemeClass(); got != c.authTh {
			t.Errorf("%s: AuthThemeClass = %q, want %q", c.name, got, c.authTh)
		}
	}
}

// TestSkin_IrisNoCookieRendersLight: a first-time visitor of an iris panel
// (no sb-t cookie) gets the light skin, in the layout and on the auth pages.
//
// Falsification (F2): in shell/chrome_state.go resolveTheme, ignore the skin
// default (`return ThemeDark` instead of `return skinDefaultTheme(skin)`) ->
// RED on the `class="light"` assertions.
func TestSkin_IrisNoCookieRendersLight(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Skin: shell.SkinIris})
	for name, html := range map[string]string{"layout": renderLayout(t, ctx, nil), "login": renderLogin(t, ctx), "mfa": renderMFA(t, ctx)} {
		if !strings.Contains(html, `class="light"`) || !strings.Contains(html, `data-skin="iris"`) {
			head := html
			if i := strings.Index(head, "<head>"); i > 0 {
				head = head[:i]
			}
			t.Errorf("%s: want class=\"light\" data-skin=\"iris\" on <html>, got head:\n%s", name, head)
		}
	}
	dark := shell.ContextWithChrome(context.Background(), shell.ChromeState{Skin: shell.SkinIris, Theme: shell.ThemeDark})
	if html := renderLayout(t, dark, nil); !strings.Contains(html, `class="dark"`) {
		t.Error("iris with an sb-t=dark cookie must render dark: the theme toggle has to keep working")
	}
}

// TestSkin_LoginAndMFACarryTheSkin: the login and MFA pages of a skinned panel
// are skinned: the attribute, the iris token block and the self-hosted fonts
// under the page's own base path.
//
// Falsification (F4): drop the data-skin attribute from LoginPage in
// shell/layout.templ (and regenerate) -> RED on LoginPage.
func TestSkin_LoginAndMFACarryTheSkin(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Skin: shell.SkinIris})
	for name, html := range map[string]string{"login": renderLogin(t, ctx), "mfa": renderMFA(t, ctx)} {
		for _, want := range []string{`data-skin="iris"`, ":root[data-skin=iris]{", ":root.dark[data-skin=iris]{", "url(/admin/static/fonts/inter-latin-wght-normal.woff2)"} {
			if !strings.Contains(html, want) {
				t.Errorf("%s page of a skinned panel lacks %q", name, want)
			}
		}
	}
}

// TestSkin_LayoutEmitsSkinOnlyWhenSkinned: fonts, tokens and structure are
// emitted for iris and for nothing else.
func TestSkin_LayoutEmitsSkinOnlyWhenSkinned(t *testing.T) {
	iris := renderLayout(t, shell.ContextWithChrome(context.Background(), shell.ChromeState{Skin: shell.SkinIris}), nil)
	for _, want := range []string{`data-skin="iris"`, "/admin/static/fonts/manrope-latin-wght-normal.woff2", "/admin/static/fonts/inter-cyrillic-wght-normal.woff2", ".card-focus"} {
		if !strings.Contains(iris, want) {
			t.Errorf("iris layout lacks %q", want)
		}
	}
	if !strings.Contains(iris, "U+0400-045F") {
		t.Error("cyrillic subset not declared")
	}
	plain := renderLayout(t, context.Background(), nil)
	if strings.Contains(plain, "fonts/") || strings.Contains(plain, "data-skin") {
		t.Error("graphite layout references skin fonts or data-skin")
	}
}

// TestSkin_FontBasePathCannotEscapeURL: the base path lands inside a CSS url();
// a hostile one falls back to the default mount rather than closing it.
func TestSkin_FontBasePathCannotEscapeURL(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{Skin: shell.SkinIris})
	var b bytes.Buffer
	ident := shell.LoginIdentifier{Label: "U", Name: "username", Type: "text", Autocomplete: "username"}
	if err := shell.LoginPage(`/x);}body{display:none}/*`, ident, "", nil).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "url(/x") || !strings.Contains(b.String(), "url(/admin/static/fonts/") {
		t.Error("hostile base path reached the font url()")
	}
}
