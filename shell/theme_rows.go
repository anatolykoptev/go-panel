package shell

import (
	"context"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/i18n"
)

// ThemeRows are the rows of the Appearance settings page: «Тема» (a segmented
// choice of the skins the panel allows, from the registry, each with a swatch;
// absent unless at least two skins are allowed) and «Режим» (light / dark, kept
// in step with the sidebar toggle). ctx is the context of the page being
// rendered: it carries the allowed skins (ChromeState.Skins), the skin and theme
// this request renders, and the panel's Strings.
//
// It lives in shell rather than components because components may import
// nothing but templ.
func ThemeRows(ctx context.Context) []components.SettingsRow {
	st := ChromeFromContext(ctx)
	s := i18n.From(ctx)
	choice := components.ThemeChoice{
		Skin:       st.SkinName(),
		Dark:       st.ThemeClass() == ThemeDark,
		LookLegend: s.Theme,
		ModeLegend: s.ThemeMode,
		LightLabel: s.ModeLight,
		DarkLabel:  s.ModeDark,
	}
	for _, sk := range st.Skins {
		choice.Skins = append(choice.Skins, components.ThemeSkin{
			Value: sk.Name,
			Label: sk.Label.For(s.Lang),
			Light: components.Swatch(sk.Light),
			Dark:  components.Swatch(sk.Dark),
		})
	}
	var rows []components.SettingsRow
	if len(choice.Skins) > 1 {
		rows = append(rows, components.SettingsRow{Label: s.Theme, Hint: s.ThemeHint, Control: components.ThemeSkinControlView(choice)})
	}
	return append(rows, components.SettingsRow{Label: s.ThemeMode, Hint: s.ModeHint, Control: components.ThemeModeControlView(choice)})
}
