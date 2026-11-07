package shell

import "context"

// GroupsCookie is the cookie persisting which nav groups are collapsed.
// Value is URL-encoded (encodeURIComponent in JS), comma-separated group names.
// Only collapsed group names are stored (non-default); absent = no groups collapsed.
// Server-readable so Layout emits data-collapsed=true at SSR time — no flash.
// Co-located with SidebarCookie because both are chrome-state cookies read by
// chromeStateFrom; chrome_state.go is the canonical home for all cookie names the
// chrome layer owns.
const GroupsCookie = "sb-g"

// ThemeCookie persists the operator's light/dark theme choice.
// Value is "light" or "dark"; absent or unrecognised = dark (backward-compat).
// Server-readable (vs localStorage) so Layout emits the theme class at SSR time —
// no flash-of-wrong-theme on first load. Mirrors SidebarCookie/GroupsCookie pattern.
const ThemeCookie = "sb-t"

// Recognised ChromeState.Theme values.
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// Recognised ChromeState.Skin values. A skin is a complete re-valuing of the
// design tokens plus structural overrides, selected by data-skin on <html>.
// SkinGraphite (the zero value) is the canon every consumer has always had and
// emits nothing extra; SkinIris is the opt-in light-first skin.
const (
	SkinGraphite = "graphite"
	SkinIris     = "iris"
)

// resolveSkin is total, mirroring themeClass: any value that is not SkinIris —
// empty, misspelt, a future skin this build does not know — resolves to
// graphite, so no caller can emit a data-skin value the stylesheet has no rules for.
func resolveSkin(skin string) string {
	if skin == SkinIris {
		return SkinIris
	}
	return SkinGraphite
}

// skinDefaultTheme is the theme a skin renders when the operator has no sb-t
// cookie and the panel configured no DefaultTheme. Graphite stays dark
// (backward-compat); iris is light-first.
func skinDefaultTheme(skin string) string {
	if resolveSkin(skin) == SkinIris {
		return ThemeLight
	}
	return ThemeDark
}

// resolveTheme picks the <html> class: a recognised cookie value wins, then a
// recognised configured default, then the skin's own default. Total: the
// result is always ThemeLight or ThemeDark.
func resolveTheme(theme, def, skin string) string {
	switch theme {
	case ThemeLight, ThemeDark:
		return theme
	}
	switch def {
	case ThemeLight, ThemeDark:
		return def
	}
	return skinDefaultTheme(skin)
}

// ChromeState carries per-request shell (chrome) configuration threaded into
// Layout via context.  It is the single context seam for all per-request
// state the layout renders, replacing the narrower SidebarState.
//
// CollapsedGroups and Profile are zero-valued in Phase 2 and will be
// populated by Phases 4 and 7 respectively.
//
// shell imports neither net/http nor auth; cookie parsing lives in the
// resource layer (resource.chromeStateFrom).
type ChromeState struct {
	Collapsed       bool
	CollapsedGroups map[string]bool
	Profile         ProfileConfig

	// Theme is the operator's light/dark choice, read from the sb-t cookie by
	// chromeStateFrom. Zero value ("") means dark — a consumer that never sets
	// Theme renders byte-identically to the pre-theme dark-only admin, which is
	// the load-bearing backward-compat invariant for downstream consumers
	// (go-grad, go-nerv, oxpulse-admin) that pick this up on a version bump.
	// Use ThemeClass() to resolve to the HTML class value.
	Theme string

	// Skin selects the visual skin ("" / unknown = graphite, "iris"). Emitted as
	// data-skin on <html> only when it resolves to a non-graphite skin, so a
	// consumer that never sets it renders byte-identically to before skins existed.
	Skin string

	// DefaultTheme is the theme rendered when there is no sb-t cookie ("" = the
	// skin's default: dark for graphite, light for iris). Set from Config.DefaultTheme.
	DefaultTheme string

	// StaticBase is the panel's static mount ("/admin/static" for BasePath
	// "/admin"), used for the skin's font URLs. "" = "/admin/static". It is read
	// only for a skinned page, so graphite output never depends on it.
	StaticBase string
}

// SkinName returns the resolved skin: SkinGraphite or SkinIris.
func (s ChromeState) SkinName() string { return resolveSkin(s.Skin) }

// SkinAttr returns the value for the data-skin attribute, "" for graphite
// (no attribute is emitted).
func (s ChromeState) SkinAttr() string {
	if k := resolveSkin(s.Skin); k != SkinGraphite {
		return k
	}
	return ""
}

// AuthThemeClass is the <html> class for the login and MFA pages. Graphite
// auth pages carry their own dark-only inline palette and always render dark
// (unchanged behaviour); a skinned auth page follows the resolved theme.
func (s ChromeState) AuthThemeClass() string {
	if resolveSkin(s.Skin) == SkinGraphite {
		return themeClass("")
	}
	return s.ThemeClass()
}

// ThemeClass returns the HTML class value for <html> given ChromeState.Theme.
// A recognised Theme wins; otherwise DefaultTheme, otherwise the skin's default
// (graphite: dark, iris: light). Always "light" or "dark".
// This is the single resolution point — Layout, LoginPage, and MFAPage all call
// it, and the falsification tests (F1/F2) mutate this function to verify the
// backward-compat invariant: an absent or garbage cookie MUST render dark.
func (s ChromeState) ThemeClass() string {
	return resolveTheme(s.Theme, s.DefaultTheme, s.Skin)
}

// themeClass resolves a raw theme string to the HTML class value.
// Returns "dark" for the zero value ("") so a consumer that never sets Theme
// renders dark (backward-compat). Returns "dark" for any value that is not
// ThemeLight — this makes the function TOTAL: no reachable path (cookie,
// direct ContextWithChrome, or a future caller) can emit a third class that
// matches neither :root.dark nor :root.light, which would fall through to the
// bare :root light palette and silently render light for an unrecognised value.
//
// The cookie validation in chromeStateFrom (resource layer) is a SEPARATE
// guard: it stops junk from being STORED in ChromeState.Theme. Both layers
// exist because each alone fails to catch a different case:
//   - themeClass alone (without chromeStateFrom validation) would still render
//     dark for junk, but ChromeState.Theme would carry the junk value — a
//     consumer inspecting state.Theme directly (not via ThemeClass) would see
//     it, and any logic branching on Theme == ThemeLight would miss it.
//   - chromeStateFrom validation alone (without total themeClass) would reject
//     junk from the cookie path, but a downstream consumer calling
//     ContextWithChrome directly with ChromeState{Theme: "purple"} would bypass
//     chromeStateFrom entirely — themeClass would return "purple" verbatim,
//     rendering an unrecognised class and falling through to light.
func themeClass(theme string) string {
	if theme == ThemeLight {
		return ThemeLight
	}
	return ThemeDark
}

// ProfileConfig carries the per-operator identity shown in the sticky-bottom
// profile block (avatar initial, name, role, settings/logout links).
//
// Zero value is safe: when Name is empty, Layout renders only the bare Logout
// link (backward-compatible with single-user HMACAuth consumers that have no
// named session).
//
// Consumers call Panel.SetProfile to supply static defaults (SettingsURL,
// LogoutURL); the resource layer overlays Name/Role per-request from the live
// session so a multi-user deployment always shows the current operator's
// identity without per-request SetProfile calls.
type ProfileConfig struct {
	// Name is the operator's display name shown in the profile block.
	// Populated per-request from Session.UserID for BcryptTOTPAuth consumers.
	// Empty → bare Logout rendered (zero-value safe, backward-compat).
	Name string

	// Role is the role label shown muted below the name (e.g. "admin", "owner").
	// Populated per-request from Session.Role.
	Role string

	// SettingsURL is the optional settings page URL. When non-empty, a
	// Settings link is rendered above Logout in the profile block.
	SettingsURL string

	// LogoutURL is the logout endpoint. When empty, defaults to /admin/logout.
	LogoutURL string
}

type chromeCtxKey struct{}

// ContextWithChrome stashes ChromeState in ctx for Layout to read.
// Chain after other context decorators (locale.WithLocale, etc.).
func ContextWithChrome(ctx context.Context, s ChromeState) context.Context {
	return context.WithValue(ctx, chromeCtxKey{}, s)
}

// ChromeFromContext returns the threaded ChromeState.
// Returns a zero value (Collapsed=false) when no state was stashed — safe default.
func ChromeFromContext(ctx context.Context) ChromeState {
	if s, ok := ctx.Value(chromeCtxKey{}).(ChromeState); ok {
		return s
	}
	return ChromeState{}
}
