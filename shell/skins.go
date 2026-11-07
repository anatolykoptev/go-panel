package shell

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// SkinCookie persists the operator's skin choice (the sidebar theme control).
// Value is a registered skin name, honoured only when the panel offers that
// skin (Config.Skins). It is NOT "sb-s": that is the submenu-collapse cookie
// admin.js already owns.
const SkinCookie = "sb-k"

// Text is a short user-visible string per UI language ("ru", "en", ...). For
// picks the entry for lang and falls back to English, so a registry entry that
// has not been translated into a panel's language still renders.
type Text map[string]string

// For returns the text for lang, else the English one, else "".
func (t Text) For(lang string) string {
	if v, ok := t[lang]; ok && v != "" {
		return v
	}
	return t["en"]
}

// Swatch is the handful of a skin's colours the theme setting draws.
type Swatch struct {
	Ground string // --bg-deep
	Tile   string // --bg-raised
	Ink    string // --text-primary
	Accent string // --accent
}

// Skin is one registry entry. The registry (Skins) is the only list of skins:
// the sidebar control, the cookie whitelist, resolveSkin and the head CSS are
// all derived from it. Adding a skin is one entry here plus its CSS.
type Skin struct {
	// Name is the data-skin value and the sb-k cookie value.
	Name string
	// Label is the option's visible name.
	Label Text
	// Light and Dark are the colours of the option's swatch in the theme
	// setting. They are copies of the skin's own tokens (an option shows a skin
	// other than the page's, so it cannot use var()); a test holds them to the
	// real token blocks.
	Light, Dark Swatch
	// DefaultTheme is the theme the skin renders for an operator with no sb-t
	// cookie and no Config.DefaultTheme.
	DefaultTheme string
	// Head emits the skin's <head> CSS for a full page (fonts, tokens, chrome);
	// staticBase is the panel's static mount. nil = the skin adds nothing
	// (graphite, the canon, is the base stylesheet itself).
	Head func(staticBase string) templ.Component
	// AuthHead is Head for the standalone login and MFA pages.
	AuthHead func(staticBase string) templ.Component
}

var skinRegistry = []Skin{
	{
		Name:         SkinGraphite,
		Label:        Text{"en": "Graphite", "ru": "Графит"},
		Light:        Swatch{Ground: "#f3f3f3", Tile: "#ffffff", Ink: "#242424", Accent: "#005fb8"},
		Dark:         Swatch{Ground: "#141414", Tile: "#1f1f1f", Ink: "#d6d6d6", Accent: "#0078d4"},
		DefaultTheme: ThemeDark,
	},
	{
		Name:         SkinIris,
		Label:        Text{"en": "Iris", "ru": "Ирис"},
		Light:        Swatch{Ground: "#eef0f6", Tile: "#ffffff", Ink: "#141729", Accent: "#5046e4"},
		Dark:         Swatch{Ground: "#0f1120", Tile: "#171a2c", Ink: "#eceef6", Accent: "#8b84ff"},
		DefaultTheme: ThemeLight,
		Head: func(base string) templ.Component {
			return concat(templ.Raw(skinFontCSS(base)), irisTokens(), irisChrome())
		},
		AuthHead: func(base string) templ.Component {
			return concat(templ.Raw(skinFontCSS(base)), irisTokens(), irisAuth())
		},
	},
}

// Skins returns the registered skins, graphite first. The slice is a copy.
func Skins() []Skin { return append([]Skin(nil), skinRegistry...) }

// LookupSkin returns the registered skin named name (exact match).
func LookupSkin(name string) (Skin, bool) {
	for _, s := range skinRegistry {
		if s.Name == name {
			return s, true
		}
	}
	return Skin{}, false
}

// SkinsNamed returns the registered skins for names, in that order, skipping
// names that are not registered or are repeated.
func SkinsNamed(names []string) []Skin {
	var out []Skin
	seen := map[string]bool{}
	for _, n := range names {
		if s, ok := LookupSkin(n); ok && !seen[n] {
			seen[n] = true
			out = append(out, s)
		}
	}
	return out
}

// concat renders the components one after another with nothing in between.
func concat(parts ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		for _, p := range parts {
			if err := p.Render(ctx, w); err != nil {
				return err
			}
		}
		return nil
	})
}
