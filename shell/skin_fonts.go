package shell

import (
	"regexp"
	"strings"
)

// safeStaticBase matches a same-origin static path prefix such as "/admin/static".
// Anything else falls back to defaultStaticBase: the value lands inside a CSS
// url(), so it must not be able to close it.
var safeStaticBase = regexp.MustCompile(`^/[A-Za-z0-9._/-]*$`)

const defaultStaticBase = "/admin/static"

// Unicode ranges are the latin and cyrillic subsets published by
// @fontsource-variable (identical to the Google Fonts subsets).
const (
	rangeLatin    = "U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD"
	rangeCyrillic = "U+0301,U+0400-045F,U+0490-0491,U+04B0-04B1,U+2116"
)

// skinFontCSS returns the <style> block with the skin's @font-face rules. It is
// only ever emitted for a skinned page, so graphite pages request no fonts.
// base is the panel's static mount ("/admin/static").
func skinFontCSS(base string) string {
	if !safeStaticBase.MatchString(base) {
		base = defaultStaticBase
	}
	base = strings.TrimRight(base, "/")
	face := func(family, file, weights, rng string) string {
		return "@font-face{font-family:'" + family + "';font-style:normal;font-display:swap;font-weight:" + weights +
			";src:url(" + base + "/fonts/" + file + ".woff2) format('woff2-variations');unicode-range:" + rng + "}"
	}
	return "<style>" +
		face("Inter Variable", "inter-latin-wght-normal", "100 900", rangeLatin) +
		face("Inter Variable", "inter-cyrillic-wght-normal", "100 900", rangeCyrillic) +
		face("Manrope Variable", "manrope-latin-wght-normal", "200 800", rangeLatin) +
		face("Manrope Variable", "manrope-cyrillic-wght-normal", "200 800", rangeCyrillic) +
		"</style>"
}
