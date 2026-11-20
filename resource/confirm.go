package resource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/anatolykoptev/go-panel/components"
)

// ErrConfirmNotFound is what a ConfirmSpec.Load returns when the subject of the
// confirmation does not exist (answered 404).
var ErrConfirmNotFound = errors.New("resource: confirm subject not found")

// ConfirmPage is what a ConfirmSpec.Load decides: the words, the before/after,
// and the POST route that performs the action. The framework adds the CSRF token.
type ConfirmPage struct {
	// Title is the page title and heading; Message is one sentence saying what
	// confirming does.
	Title   string
	Message string
	// Diff is the current-versus-target view.
	Diff components.Diff
	// Action is the POST route confirming submits, a same-origin path (a
	// MountAction route, so the CSRF check and role gate apply to it).
	// Fields are extra hidden inputs the action reads (ids, the target value).
	Action string
	Fields []components.HiddenField
	// ConfirmLabel and CancelLabel are the button texts (defaults: the panel
	// Strings' Confirm and Cancel). CancelHref is where Cancel goes (default:
	// the panel index).
	ConfirmLabel string
	CancelLabel  string
	CancelHref   string
	// Danger styles the confirm button as destructive.
	Danger bool
	// NavID marks a sidebar item active ("" = none).
	NavID string
}

// ConfirmSpec declares a confirmation page, MountPage's twin for "are you sure,
// here is exactly what changes". Mount the action it posts to with MountAction.
type ConfirmSpec struct {
	// Path is the URL suffix under BasePath, like PageSpec.Path ("settings/revert"
	// serves GET {basePath}/settings/revert/). Required.
	Path string
	// RequiredRole gates the page like PageSpec.RequiredRole.
	RequiredRole string
	// Load builds the page for this request (query values carry the subject:
	// an id, the target revision). It runs inside the panel guard, so the
	// session, role and tenant are already checked. ErrConfirmNotFound -> 404;
	// any other error -> a generic 500 (detail is logged).
	Load func(ctx context.Context, r *http.Request) (ConfirmPage, error)
}

// MountConfirm serves a confirmation page: GET only, behind the panel guard,
// rendering components.ConfirmView with a fresh CSRF token for the action. It
// needs Config.CSRFKey and a session-cookie authenticator, checked at mount
// (fail-closed) like MountAction. Call it before Handler().
//
// The page is how a row action shows "current vs target" before the owner
// commits: point the action's link at it, and let its form post to the real
// MountAction route.
func (p *Panel) MountConfirm(spec ConfirmSpec) {
	if spec.Load == nil {
		panic("resource: MountConfirm requires a non-nil Load")
	}
	if strings.Trim(spec.Path, "/") == "" {
		panic("resource: MountConfirm requires a non-empty Path")
	}
	validateCSRFConfig(p, fmt.Sprintf("resource: MountConfirm %q", spec.Path), "", "confirm pages")

	p.MountPage(PageSpec{
		Path:         spec.Path,
		RequiredRole: spec.RequiredRole,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			page, err := spec.Load(r.Context(), r)
			if err != nil {
				if errors.Is(err, ErrConfirmNotFound) {
					http.NotFound(w, r)
					return
				}
				slog.ErrorContext(r.Context(), "confirm: load failed", "path", spec.Path, "err", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			if !samePath(page.Action) || (page.CancelHref != "" && !samePath(page.CancelHref)) {
				slog.ErrorContext(r.Context(), "confirm: action and cancel must be same-origin paths", "path", spec.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			s := p.strings
			c := components.Confirm{
				Title: page.Title, Message: page.Message, Diff: page.Diff,
				Post: components.PostAction{
					URL: page.Action, Token: p.CSRFToken(r), Fields: page.Fields,
				},
				ConfirmLabel: firstNonEmpty(page.ConfirmLabel, s.Confirm),
				CancelLabel:  firstNonEmpty(page.CancelLabel, s.Cancel),
				CancelHref:   firstNonEmpty(page.CancelHref, p.basePath+"/"),
				Danger:       page.Danger,
			}
			if err := p.RenderPage(w, r, page.Title, page.NavID, components.ConfirmView(c)); err != nil {
				slog.ErrorContext(r.Context(), "confirm: render failed", "path", spec.Path, "err", err)
			}
		},
	})
}

// samePath reports whether u is a same-origin absolute path: it starts with a
// single "/", so it can neither name another host ("//host", "https://...") nor
// smuggle a backslash form some browsers read as "//".
func samePath(u string) bool {
	return strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") && !strings.Contains(u, `\`)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
