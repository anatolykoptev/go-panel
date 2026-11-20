package resource

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/csrf"
)

// RowAction is a per-row POST button declared once on a [Resource] and rendered
// by the framework, so a consumer never hand-writes a form, a CSRF field or a
// confirm prompt for it.
//
// Declare it in [Resource.RowActions]; a row opts in by listing the Key in
// [Row.Actions]. The framework renders a real <form method="post"> carrying a
// CSRF token bound to the session (the same token MountAction verifies) and,
// when Confirm yields text, a data-confirm attribute that the shell's admin.js
// turns into a confirm() prompt before the form submits. There is no inline
// handler, so the strict page CSP is unaffected.
//
// The target route is the consumer's own: mount it with [Panel.MountAction].
// Register panics when RowActions is used on a panel that cannot issue CSRF
// tokens, so a declared action can never render as a silently unprotected or
// dead button.
type RowAction struct {
	// Key identifies the action in [Row.Actions]. Required, unique per resource.
	Key string
	// Label is the button text. Required.
	Label string
	// Path returns the POST route for a row, RELATIVE to the panel base path
	// (as in [ActionSpec.Path]): "history/"+url.PathEscape(id)+"/revert". The
	// framework prefixes the base path, so the result can never leave the panel
	// and the consumer never hardcodes where it is mounted. Escape the id: it is
	// a database value. Required.
	Path func(id string) string
	// Confirm, when set, returns the confirm prompt for a row. An empty result
	// means no prompt for that row.
	Confirm func(Row) string
	// Tone selects the button style from the closed [components.Tone] set:
	// ToneRed is the destructive style, ToneBlue and ToneGreen the primary one,
	// everything else the quiet secondary default.
	Tone components.Tone
}

// validateRowActions panics at Register time (fail-closed) on a malformed
// RowActions declaration, and when the panel could not issue the CSRF token
// the rendered forms need.
func validateRowActions(p *Panel, r Resource) {
	if len(r.RowActions) == 0 {
		return
	}
	validateCSRFConfig(p, fmt.Sprintf("resource.Register %q", r.Name), "RowActions is set but ", "row actions")
	seen := make(map[string]bool, len(r.RowActions))
	for i, a := range r.RowActions {
		switch {
		case a.Key == "":
			panic(fmt.Sprintf("resource.Register %q: RowActions[%d] has an empty Key", r.Name, i))
		case seen[a.Key]:
			panic(fmt.Sprintf("resource.Register %q: RowActions[%d] duplicates Key %q", r.Name, i, a.Key))
		case a.Label == "":
			panic(fmt.Sprintf("resource.Register %q: RowActions[%d] %q has an empty Label", r.Name, i, a.Key))
		case a.Path == nil:
			panic(fmt.Sprintf("resource.Register %q: RowActions[%d] %q has a nil Path", r.Name, i, a.Key))
		}
		seen[a.Key] = true
	}
}

// issueActionToken gives the list the CSRF token its RowActions forms post. It
// is a separate field from the delete/undo token (listPageData.CSRFToken,
// gated on Writer.Restore) so the two gates never share a condition. A resource
// without RowActions gets no token.
func (p *Panel) issueActionToken(r Resource, req *http.Request, data *listPageData) {
	if len(r.RowActions) == 0 {
		return
	}
	data.ActionToken = csrf.Issue(p.csrfKey, p.sessionValue(req), csrf.DefaultTTL)
}

// rowActionsFor resolves a row's opted-in keys against the declared actions,
// in the row's own order. A key that is not declared is skipped with a warning
// rather than rendered: a row cannot make the framework draw a button the
// resource never declared.
func rowActionsFor(r Resource, row Row) []RowAction {
	if len(row.Actions) == 0 {
		return nil
	}
	out := make([]RowAction, 0, len(row.Actions))
	for _, key := range row.Actions {
		found := false
		for _, a := range r.RowActions {
			if a.Key == key {
				out = append(out, a)
				found = true
				break
			}
		}
		if !found {
			slog.Warn("resource: row opts into an undeclared RowAction", "resource", r.Name, "key", key)
		}
	}
	return out
}

// rowActionHref is the absolute POST target of one action for one row.
func rowActionHref(d listPageData, a RowAction, row Row) string {
	return d.BasePath + "/" + strings.TrimLeft(a.Path(row.ID), "/")
}

// rowActionConfirm is the data-confirm attribute set for one action and row.
func rowActionConfirm(a RowAction, row Row) templ.Attributes {
	if a.Confirm == nil {
		return nil
	}
	msg := a.Confirm(row)
	if msg == "" {
		return nil
	}
	return templ.Attributes{"data-confirm": msg}
}

// rowActionClass maps the closed Tone set to the shared button variants.
func rowActionClass(t components.Tone) string {
	switch t {
	case components.ToneRed:
		return "btn btn-sm btn-danger"
	case components.ToneBlue, components.ToneGreen:
		return "btn btn-sm btn-primary"
	default:
		return "btn btn-sm btn-secondary"
	}
}
