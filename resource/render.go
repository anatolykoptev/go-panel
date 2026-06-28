package resource

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/shell"
)

// RenderPage renders content inside the panel shell (chrome + sidebar with
// activeID marked active), setting the HTML content-type and the standard admin
// security headers (CSP, X-Frame-Options, Cache-Control — same as resource page
// handlers). For consumers mounting bespoke routes alongside the resource
// framework. activeID matches a nav item's ID (use AddNav to register the entry);
// pass "" for no active highlight.
func (p *Panel) RenderPage(w http.ResponseWriter, r *http.Request, title, activeID string, content templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	shell.SecurityHeaders(w)
	ctx := shell.ContextWithChrome(r.Context(), chromeStateFrom(r))
	return shell.Layout(title, p.NavItemsActive(activeID), content).Render(ctx, w)
}

// chromeStateFrom reads the collapse cookies from the request and returns a
// ChromeState.
//
//   - Cookie "sb-c"="1" → Collapsed=true; absent or other value → false.
//   - Cookie "sb-g"=<url-encoded comma-separated names> → CollapsedGroups map;
//     absent or empty → nil map (no groups collapsed).
//
// Encoding contract: the whole sb-g value is URL-encoded (encodeURIComponent on
// the joined string), so the server URL-unescapes the value before splitting on ','.
// Consequence: group names must not contain literal commas — a comma in a name
// would become an unescaped delimiter after decode, splitting one name into two
// phantom entries. Group labels are developer-defined; commas are forbidden by
// convention. See matching note in admin.js readGroups/writeGroups.
//
// Lives in the resource layer (not shell) so shell stays net/http-free.
func chromeStateFrom(r *http.Request) shell.ChromeState {
	var state shell.ChromeState
	if c, err := r.Cookie(shell.SidebarCookie); err == nil && c.Value == "1" {
		state.Collapsed = true
	}
	if c, err := r.Cookie(shell.GroupsCookie); err == nil && c.Value != "" {
		raw, decErr := url.QueryUnescape(c.Value)
		if decErr != nil {
			raw = c.Value // use verbatim if decode fails
		}
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if state.CollapsedGroups == nil {
				state.CollapsedGroups = make(map[string]bool)
			}
			state.CollapsedGroups[name] = true
		}
	}
	return state
}

// RenderPageHTML is RenderPage for callers holding already-rendered,
// ALREADY-ESCAPED HTML (e.g. produced via html/template). The string is
// wrapped with templ.Raw and emitted verbatim - the caller is responsible for
// escaping.
//
// SECURITY: templ.Raw bypasses templ's context-aware auto-escaping. Never pass
// unescaped user-controlled or database-sourced strings to this function; doing
// so introduces XSS. Only pass strings produced by html/template execution or
// other provably safe HTML renderers.
func (p *Panel) RenderPageHTML(w http.ResponseWriter, r *http.Request, title, activeID, htmlContent string) error {
	return p.RenderPage(w, r, title, activeID, templ.Raw(htmlContent))
}
