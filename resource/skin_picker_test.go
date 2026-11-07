package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

// pickerPanel is a single-user panel with the skin choice on: def is
// Config.Skin, skins is Config.Skins.
func pickerPanel(def string, skins []string) (*resource.Panel, *auth.HMACAuth) {
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw", HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/admin"})
	return resource.New(resource.Config{Title: "Picker Panel", BasePath: "/admin", Auth: a, Skin: def, Skins: skins}), a
}

func skinCookie(v string) *http.Cookie { return &http.Cookie{Name: shell.SkinCookie, Value: v} }

func skinAttr(html string) string {
	i := strings.Index(html, `data-skin="`)
	if i < 0 {
		return ""
	}
	rest := html[i+len(`data-skin="`):]
	end, _, _ := strings.Cut(rest, `"`)
	return end
}

// F1: the whitelist. An operator (or a stale cookie from a sibling panel on the
// same host) sends a value the panel did not offer and sees the panel default,
// on the layout and on login. The default is iris on purpose: against a
// graphite default a mutant that echoes the raw cookie would still render
// graphite (resolveSkin is total) and could not go red.
//
// Falsification (F1): in resource/render.go skinFor, return c.Value when it is
// non-empty -> RED on "evil" and on the not-offered cases.
func TestPicker_ForgedSkinCookieFallsBackToTheDefault(t *testing.T) {
	iris, _ := pickerPanel(shell.SkinIris, []string{shell.SkinIris, shell.SkinGraphite})
	for _, forged := range []string{"evil", "IRIS", " iris", "iris ", "iris,graphite", `iris"><script>`, "neon", ""} {
		for name, html := range map[string]string{"layout": renderSkinned(t, iris, skinCookie(forged)), "login": getLogin(t, iris, skinCookie(forged))} {
			if got := skinAttr(html); got != "iris" {
				t.Errorf("sb-k=%q: %s rendered data-skin=%q, want the default \"iris\"", forged, name, got)
			}
			if strings.Contains(html, `iris"><script>`) {
				t.Errorf("sb-k=%q reached the markup of the %s", forged, name)
			}
		}
	}
	// Registered but not offered: an iris cookie (valid on another panel) must
	// not switch a panel that lists only graphite.
	gOnly, _ := pickerPanel(shell.SkinGraphite, []string{shell.SkinGraphite})
	if got := skinAttr(renderSkinned(t, gOnly, skinCookie("iris"))); got != "" {
		t.Errorf("a skin the panel does not offer was honoured: data-skin=%q", got)
	}
	if got := skinAttr(getLogin(t, gOnly, skinCookie("iris"))); got != "" {
		t.Errorf("login honoured a skin the panel does not offer: data-skin=%q", got)
	}
}

// A consumer that sets no Skins sees no change, whatever cookies an operator
// carries.
//
// Falsification: in resource/skins.go offeredSkins, return shell.SkinsNamed of
// every registered name when names is empty -> RED.
func TestPicker_Off_ChangesNothing(t *testing.T) {
	for name, def := range map[string]string{"graphite panel": "", "iris panel": shell.SkinIris} {
		p, _ := pickerPanel(def, nil)
		baseLayout, baseLogin := renderSkinned(t, p), getLogin(t, p)
		for _, c := range []*http.Cookie{skinCookie("iris"), skinCookie("graphite")} {
			if got := renderSkinned(t, p, c); got != baseLayout {
				t.Errorf("%s: cookie %s=%s changed the layout of a panel without Skins", name, c.Name, c.Value)
			}
			if got := getLogin(t, p, c); got != baseLogin {
				t.Errorf("%s: cookie %s=%s changed the login of a panel without Skins", name, c.Name, c.Value)
			}
		}
	}
}

// F3: picking Графит on an iris panel (and the reverse) holds across requests
// on the layout and the login screen. The reverse is the case where the
// middleware that carries the skin to login must be on although the panel's
// own skin is not.
//
// Falsification (F3): in resource/render.go chromeStateFrom, set
// `state.Skin = p.skin` (ignore the cookie) -> RED.
func TestPicker_ChoicePersistsAcrossReloadsAndPages(t *testing.T) {
	for _, tc := range []struct{ name, def, pick, wantAttr string }{
		{"graphite picked on an iris panel", shell.SkinIris, shell.SkinGraphite, ""},
		{"iris picked on a graphite panel", shell.SkinGraphite, shell.SkinIris, "iris"},
		{"iris picked on a panel with no Skin at all", "", shell.SkinIris, "iris"},
	} {
		p, _ := pickerPanel(tc.def, []string{shell.SkinIris, shell.SkinGraphite})
		for reload := range 2 {
			c := skinCookie(tc.pick)
			if got := skinAttr(renderSkinned(t, p, c)); got != tc.wantAttr {
				t.Errorf("%s (request %d): layout data-skin=%q, want %q", tc.name, reload, got, tc.wantAttr)
			}
			login := getLogin(t, p, c)
			if got := skinAttr(login); got != tc.wantAttr {
				t.Errorf("%s (request %d): login data-skin=%q, want %q", tc.name, reload, got, tc.wantAttr)
			}
			if tc.wantAttr == "iris" && !strings.Contains(login, "/admin/static/fonts/inter-latin-wght-normal.woff2") {
				t.Errorf("%s: login carries the data-skin but not the iris CSS", tc.name)
			}
		}
	}
}

// RenderError renders from the context the panel built for the request, so a
// consumer that passes r.Context() gets the operator's skin.
func TestPicker_RenderErrorFollowsTheRequestCookie(t *testing.T) {
	p, a := pickerPanel(shell.SkinGraphite, []string{shell.SkinGraphite, shell.SkinIris})
	var body string
	p.MountPage(resource.PageSpec{Path: "boom", Handler: func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		p.RenderError(r.Context(), rec, "T", nil, templ.Raw("<p>x</p>"), "x", "boom")
		body = rec.Body.String()
		w.WriteHeader(http.StatusNoContent)
	}})
	get := func(c ...*http.Cookie) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/boom/", nil)
		req.AddCookie(i18nHMACCookie(t, a))
		for _, ck := range c {
			req.AddCookie(ck)
		}
		p.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}
	get(skinCookie("iris"))
	if skinAttr(body) != "iris" {
		t.Errorf("RenderError ignored the operator's skin: data-skin=%q", skinAttr(body))
	}
	get()
	if skinAttr(body) != "" {
		t.Errorf("RenderError without a cookie must render the panel default, got data-skin=%q", skinAttr(body))
	}
}
