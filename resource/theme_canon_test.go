package resource_test

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Guards for the fleet admin canon v2.1 (graphite dark + graphite light).
// Everything is parsed out of the stylesheet the panel actually serves, so the
// guards cannot drift from the CSS: the ground truth is the rendered page, not
// a list written next to the test.

// canonThemes returns the effective custom properties of each theme. CSS
// cascades :root.dark over the bare :root on the same element, so the dark map
// is the light map overlaid with the dark block — which is exactly how a token
// declared once (an alias such as --text-muted:var(--text-secondary)) resolves
// to the dark value in dark.
func canonThemes(t *testing.T) map[string]map[string]string {
	t.Helper()
	body := renderedStyles(t)
	light := blockTokens(t, body, ":root{")
	dark := map[string]string{}
	for k, v := range light {
		dark[k] = v
	}
	for k, v := range blockTokens(t, body, ":root.dark{") {
		dark[k] = v
	}
	return map[string]map[string]string{"light": light, "dark": dark}
}

var varRef = regexp.MustCompile(`^var\((--[a-zA-Z0-9_-]+)\)$`)

// resolveColor follows var() chains inside one theme and returns the literal
// value ("" when a link is undefined — the silent-drop case this repo bans).
func resolveColor(theme map[string]string, v string) string {
	v = strings.TrimSpace(v)
	for i := 0; i < 8; i++ {
		m := varRef.FindStringSubmatch(v)
		if m == nil {
			return v
		}
		v = strings.TrimSpace(theme[m[1]])
	}
	return ""
}

// hex6 normalises #rgb / #rrggbb; ok=false for anything else (rgba, names, "").
func hex6(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if !strings.HasPrefix(v, "#") {
		return "", false
	}
	h := v[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return "", false
	}
	if _, err := strconv.ParseUint(h, 16, 32); err != nil {
		return "", false
	}
	return "#" + h, true
}

func mustHex(t *testing.T, theme string, tokens map[string]string, v string) string {
	t.Helper()
	r := resolveColor(tokens, v)
	h, ok := hex6(r)
	if !ok {
		t.Errorf("%s: %q does not resolve to an opaque hex colour (got %q)", theme, v, r)
	}
	return h
}

func tokRef(name string) string {
	if strings.HasPrefix(name, "#") {
		return name
	}
	return "var(" + name + ")"
}

// G1 — every declared text/background pair clears 4.5:1 in BOTH themes.
//
// Falsification: set the dark --text-secondary to #5a5a5a in shell/styles.templ
// (:root.dark block) -> secondary text on every ground drops below 4.5 -> RED.
func TestCanon_G1_DeclaredPairsAA(t *testing.T) {
	grounds := []string{"--bg-deep", "--bg-base", "--bg-raised", "--bg-elevated"}
	status := []string{"positive", "warning", "negative", "info", "special", "neutral"}
	type pair struct{ fg, bg string }
	var pairs []pair
	for _, fg := range []string{"--text-primary", "--text-secondary", "--accent-text"} {
		for _, bg := range grounds {
			pairs = append(pairs, pair{fg, bg})
		}
	}
	pairs = append(pairs, pair{"--accent-text", "--accent-soft"})
	for _, s := range status {
		pairs = append(pairs, pair{"--" + s, "--" + s + "-bg"}, pair{"--" + s, "--bg-raised"}, pair{"--" + s, "--bg-base"})
	}
	pairs = append(pairs,
		pair{"#ffffff", "--accent"}, pair{"#ffffff", "--pm7-primary"}, pair{"#ffffff", "--btn-primary-bg"},
		pair{"#ffffff", "--btn-primary-hover"}, pair{"#ffffff", "--btn-danger-bg"}, pair{"#ffffff", "--btn-danger-hover"},
		pair{"--pm7-foreground", "--pm7-background"}, pair{"--pm7-muted-foreground", "--pm7-background"},
		pair{"--pm7-muted-foreground", "--pm7-muted"}, pair{"--pm7-foreground", "--pm7-input"},
	)
	for _, fam := range []string{"fit-strong", "fit-moderate", "fit-weak", "fit-low", "fit-reject", "fit-unscored", "suc-strong", "suc-moderate", "suc-longshot"} {
		pairs = append(pairs, pair{"--" + fam + "-fg", "--" + fam + "-bg"})
	}
	for name, tokens := range canonThemes(t) {
		for _, p := range pairs {
			fg, bg := mustHex(t, name, tokens, tokRef(p.fg)), mustHex(t, name, tokens, tokRef(p.bg))
			if fg == "" || bg == "" {
				continue
			}
			if r := wcagContrastRatio(fg, bg); r < 4.5 {
				t.Errorf("%s: %s (%s) on %s (%s) = %.2f:1 < 4.5", name, p.fg, fg, p.bg, bg, r)
			}
		}
	}
}

// cssRule is one `selector{decls}` leaf of the served stylesheet.
type cssRule struct{ sel, body string }

var ruleRe = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

// styleRules returns the leaf rules of the inline stylesheet, comments removed,
// excluding the two theme token blocks.
func styleRules(t *testing.T) []cssRule {
	t.Helper()
	body := renderedStyles(t)
	i, j := strings.Index(body, "<style>"), strings.Index(body, "</style>")
	if i < 0 || j < i {
		t.Fatal("no inline <style> in rendered page")
	}
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(body[i+len("<style>"):j], "")
	var out []cssRule
	for _, m := range ruleRe.FindAllStringSubmatch(css, -1) {
		sel := strings.TrimSpace(m[1])
		if strings.HasPrefix(sel, ":root") {
			continue
		}
		out = append(out, cssRule{sel, m[2]})
	}
	if len(out) < 100 {
		t.Fatalf("parsed only %d rules; the rule parser no longer matches the stylesheet", len(out))
	}
	return out
}

func decl(body, prop string) string {
	re := regexp.MustCompile(`(?:^|;)\s*` + regexp.QuoteMeta(prop) + `\s*:\s*([^;]+)`)
	if m := re.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// G1b — the pairs are also DERIVED from the stylesheet: every rule that sets
// both a background and a text colour (token or hex literal) is checked, so a
// new component cannot ship a pair the hand-written list above never heard of.
//
// Falsification: in shell/styles.templ change .badge-red to
// color:var(--text-faint) -> faint on --negative-bg is ~2.4:1 -> RED.
func TestCanon_G1b_RuleDerivedPairsAA(t *testing.T) {
	rules := styleRules(t)
	checked := 0
	for name, tokens := range canonThemes(t) {
		for _, r := range rules {
			bg := decl(r.body, "background")
			if bg == "" {
				bg = decl(r.body, "background-color")
			}
			fg := decl(r.body, "color")
			if bg == "" || fg == "" {
				continue
			}
			fgH, ok1 := hex6(resolveColor(tokens, fg))
			bgH, ok2 := hex6(resolveColor(tokens, bg))
			if !ok1 || !ok2 {
				continue // transparent / inherit / rgba: not a pair we can compute
			}
			checked++
			if ratio := wcagContrastRatio(fgH, bgH); ratio < 4.5 {
				t.Errorf("%s: rule %q: color %s on background %s = %.2f:1 < 4.5", name, r.sel, fg, bg, ratio)
			}
		}
	}
	if checked < 60 {
		t.Fatalf("only %d rule-derived pairs checked; the extraction is no longer seeing the stylesheet", checked)
	}
}

// legacyTokens is the contract of v0.23.17: every custom property a consumer
// could have used then (the navy-era vocabulary) must keep resolving.
var legacyTokens = []string{
	"--bg-deep", "--bg-base", "--bg-surface", "--bg-elevated", "--border", "--border-subtle",
	"--text-primary", "--text-secondary", "--text-muted", "--text-label",
	"--accent", "--accent-link", "--accent-glow", "--accent-dim", "--accent-light",
	"--green", "--green-dim", "--green-light", "--yellow", "--yellow-dim", "--yellow-light",
	"--red", "--red-dim", "--red-light", "--purple", "--purple-dim", "--purple-light",
	"--teal", "--teal-dim", "--orange", "--orange-dim", "--orange-light",
	"--font-sans", "--font-mono", "--radius", "--radius-lg",
	"--btn-primary-bg", "--btn-primary-hover", "--btn-danger-bg", "--btn-danger-hover",
	"--badge-gray-fg", "--row-hover-bg", "--overlay-bg",
	"--fit-strong-bg", "--fit-strong-fg", "--fit-moderate-bg", "--fit-moderate-fg", "--fit-weak-bg", "--fit-weak-fg",
	"--fit-low-bg", "--fit-low-fg", "--fit-reject-bg", "--fit-reject-fg", "--fit-unscored-bg", "--fit-unscored-fg",
	"--suc-strong-bg", "--suc-strong-fg", "--suc-moderate-bg", "--suc-moderate-fg", "--suc-longshot-bg", "--suc-longshot-fg",
	"--ou-over-fg", "--ou-under-fg",
	"--pm7-primary", "--pm7-primary-hover", "--pm7-primary-active", "--pm7-primary-foreground", "--pm7-background",
	"--pm7-foreground", "--pm7-muted", "--pm7-muted-foreground", "--pm7-border", "--pm7-border-hover",
	"--pm7-success", "--pm7-warning", "--pm7-error", "--pm7-radius", "--pm7-surface", "--pm7-surface-hover",
	"--pm7-input",
}

var canonTokens = []string{
	"--bg-raised", "--border-subtle", "--text-faint", "--accent-text", "--accent-soft",
	"--positive", "--positive-bg", "--warning", "--warning-bg", "--negative", "--negative-bg",
	"--info", "--info-bg", "--special", "--special-bg", "--neutral", "--neutral-bg",
}

var varUse = regexp.MustCompile(`var\((--[a-zA-Z0-9_-]+)`)

// G2 — every legacy and canon token name resolves to a value in both themes,
// and so does every var() the stylesheet itself uses (derived from the CSS, so
// a rule cannot reference a token nobody defined).
//
// Falsification: delete `--text-label:var(--text-secondary);` from the light
// block in shell/styles.templ -> light AND dark lose it -> RED.
func TestCanon_G2_TokensResolveBothThemes(t *testing.T) {
	themes := canonThemes(t)
	used := map[string]bool{}
	for _, r := range styleRules(t) {
		for _, m := range varUse.FindAllStringSubmatch(r.body, -1) {
			used[m[1]] = true
		}
	}
	want := map[string]bool{}
	for _, n := range legacyTokens {
		want[n] = true
	}
	for _, n := range canonTokens {
		want[n] = true
	}
	for n := range used {
		if strings.HasPrefix(n, "--tone-") {
			continue // set per-element by the .tone-* classes, not by the theme
		}
		if strings.HasPrefix(n, "--pm7-") {
			continue // pm7.css owns the tail of its own vocabulary and its fallbacks
		}
		want[n] = true
	}
	for name, tokens := range themes {
		for n := range want {
			v, ok := tokens[n]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s: token %s is not defined", name, n)
				continue
			}
			if r := resolveColor(tokens, v); r == "" {
				t.Errorf("%s: token %s = %q does not resolve (broken var() chain)", name, n, v)
			}
		}
	}
}

func hueSat(h string) (hue, sat float64) {
	r := float64(mustParse(h[1:3])) / 255
	g := float64(mustParse(h[3:5])) / 255
	b := float64(mustParse(h[5:7])) / 255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l := (mx + mn) / 2
	d := mx - mn
	if d == 0 {
		return 0, 0
	}
	if l > 0.5 {
		sat = d / (2 - mx - mn)
	} else {
		sat = d / (mx + mn)
	}
	switch mx {
	case r:
		hue = math.Mod((g-b)/d, 6)
	case g:
		hue = (b-r)/d + 2
	default:
		hue = (r-g)/d + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}
	return hue, sat
}

func mustParse(h string) int64 {
	v, _ := strconv.ParseInt(h, 16, 32)
	return v
}

var (
	colourHex = regexp.MustCompile(`#[0-9a-fA-F]{6}\b|#[0-9a-fA-F]{3}\b`)
	rgbFn     = regexp.MustCompile(`rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)`)
	alphaFn   = regexp.MustCompile(`rgba\(|hsla\(|rgb\([^)]*/`)
)

// G3 — no translucent background on a badge-like surface, and no violet
// anywhere in the stylesheet.
//
// Falsification: give .badge-red an rgba(...) background in shell/styles.templ
// -> the badge rule matches alphaFn -> RED. Change dark --special to #a78bfa
// (hue 255 / sat 92%) -> the violet scan -> RED.
func TestCanon_G3_NoRGBABadgesNoViolet(t *testing.T) {
	surfaceSel := regexp.MustCompile(`badge|notice|\.delta|\.fit-|\.suc-|chip`)
	themes := canonThemes(t)
	for _, r := range styleRules(t) {
		if !surfaceSel.MatchString(r.sel) {
			continue
		}
		for _, prop := range []string{"background", "background-color"} {
			v := decl(r.body, prop)
			if v == "" {
				continue
			}
			if alphaFn.MatchString(v) {
				t.Errorf("rule %q: translucent %s: %s (badge/notice backgrounds must be solid)", r.sel, prop, v)
			}
			for name, tokens := range themes {
				if alphaFn.MatchString(resolveColor(tokens, v)) {
					t.Errorf("%s: rule %q: %s: %s resolves to a translucent colour", name, r.sel, prop, v)
				}
			}
		}
	}
	for _, tokenAlpha := range []string{"--info-bg", "--positive-bg", "--warning-bg", "--negative-bg", "--special-bg", "--neutral-bg"} {
		for name, tokens := range themes {
			if alphaFn.MatchString(tokens[tokenAlpha]) {
				t.Errorf("%s: %s is translucent: %s", name, tokenAlpha, tokens[tokenAlpha])
			}
		}
	}

	body := renderedStyles(t)
	i, j := strings.Index(body, "<style>"), strings.Index(body, "</style>")
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(body[i:j], "")
	seen := 0
	check := func(h, ctx string) {
		seen++
		if hue, sat := hueSat(h); hue >= 250 && hue <= 300 && sat > 0.25 {
			t.Errorf("violet colour %s (hue %.0f, sat %.0f%%) in %s", h, hue, sat*100, ctx)
		}
	}
	for _, m := range colourHex.FindAllString(css, -1) {
		if h, ok := hex6(m); ok {
			check(h, "stylesheet")
		}
	}
	for _, m := range rgbFn.FindAllStringSubmatch(css, -1) {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		check(fmt.Sprintf("#%02x%02x%02x", a, b, c), "stylesheet rgb()")
	}
	if seen < 80 {
		t.Fatalf("violet scan saw only %d colours; the scan no longer covers the stylesheet", seen)
	}
}

// Nothing below 11px (canon floor): every font-size declared in the
// stylesheet is >= .6875rem / 11px.
//
// Falsification: set .sidebar-count to font-size:.625rem -> RED.
func TestCanon_TypeFloor11px(t *testing.T) {
	re := regexp.MustCompile(`font-size\s*:\s*([0-9.]+)(rem|px|em)`)
	n := 0
	for _, r := range styleRules(t) {
		for _, m := range re.FindAllStringSubmatch(r.body, -1) {
			v, _ := strconv.ParseFloat(m[1], 64)
			px := v
			if m[2] != "px" {
				px = v * 16
			}
			n++
			if m[2] == "em" {
				continue // relative to a parent; the parent is checked
			}
			if px < 11 {
				t.Errorf("rule %q: font-size %s%s = %.1fpx < 11px", r.sel, m[1], m[2], px)
			}
		}
	}
	if n < 40 {
		t.Fatalf("only %d font-size declarations seen", n)
	}
}
