package resource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/anatolykoptev/go-panel/locale"
	"github.com/anatolykoptev/go-panel/tenant"
)

// ProposeResult is the outcome of [Panel.Propose].
type ProposeResult struct {
	// Errors maps a field key to its validation message. Non-empty means the
	// values were rejected and Writer.Save was NOT called (or Save returned a
	// *SaveError, whose message is filed under its field).
	Errors map[string]string
}

// Accepted reports whether the values passed validation and Writer.Save
// succeeded.
func (r ProposeResult) Accepted() bool { return len(r.Errors) == 0 }

// Propose submits values for r exactly as the HTTP save route does after its
// transport checks (body limit, CSRF, locale from the query): resolve dynamic
// select options, collect the form fields, merge PresetValues on create,
// validate every field, then Writer.Save and AfterSave. id=="" creates.
//
// It exists so a non-browser caller (the MCP propose tool) reaches the
// consumer's Writer through the same validation as the admin form and never
// through a side door. The caller owns authentication, role and tenant
// resolution: t must be the already-resolved tenant, and ctx the context to
// hand the Writer. The panel's session-bound CSRF token does not apply — it
// defends ambient browser credentials, and the bearer a programmatic caller
// presents is not ambient.
//
// A validation failure or a *SaveError is reported in the result, not as an
// error. Any other Save error is returned as-is (ErrDetailNotFound included),
// after AfterSave has seen it.
func (p *Panel) Propose(ctx context.Context, r Resource, t tenant.Tenant, id string, values map[string]string) (ProposeResult, error) {
	if r.Writer == nil {
		return ProposeResult{}, fmt.Errorf("resource %q has no Writer", r.Name)
	}
	creating := id == ""

	// The default locale, as an HTTP save without ?locale= resolves it
	// (create always lands there; see resolveSaveLocale).
	loc := p.locales.Resolve("")
	multi := !creating && p.multiLocale()
	ctx = locale.WithLocale(ctx, loc)

	rr, err := withResolvedForm(r, ctx, t)
	if err != nil {
		return ProposeResult{}, fmt.Errorf("resolve options: %w", err)
	}
	fields := rr.Writer.Form.localeFields(loc, p.locales.Default, multi)

	// collectFormValues reads a request; hand it the values as a parsed form so
	// checkbox normalisation (and anything added to it later) is shared, not
	// copied.
	form := make(url.Values, len(values))
	for k, v := range values {
		form.Set(k, v)
	}
	collected := collectFormValues(&http.Request{Form: form, PostForm: form}, fields)

	if err := mergePresetValues(rr, ctx, t, creating, collected); err != nil {
		return ProposeResult{}, fmt.Errorf("preset values: %w", err)
	}
	if errs := validateFields(p.strings, fields, collected); errs.hasErrors() {
		return ProposeResult{Errors: errs}, nil
	}

	saveErr := rr.Writer.Save(ctx, t, id, collected)
	if rr.Writer.AfterSave != nil {
		rr.Writer.AfterSave(ctx, id, saveErr)
	}
	if saveErr == nil {
		return ProposeResult{}, nil
	}
	var se *SaveError
	if errors.As(saveErr, &se) {
		return ProposeResult{Errors: map[string]string{se.Field: se.Message}}, nil
	}
	slog.Error("resource: propose save failed", "resource", r.Name, "err", saveErr)
	return ProposeResult{}, saveErr
}
