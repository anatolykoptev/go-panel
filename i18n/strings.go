// Package i18n holds the words go-panel's own chrome speaks: buttons, headings,
// empty states, validation messages, the login page. It is the UI-language axis
// of the panel, orthogonal to package locale (which picks WHICH language of a
// row's translatable fields is edited).
//
// Strings has English defaults — the exact text the panel always rendered — so
// a consumer that configures nothing sees no change. A consumer overrides any
// subset: fields left at their zero value fall back to Default field by field.
// Russian() is a ready preset.
//
// The type lives in this leaf package, below shell, auth and resource, because
// all three render text: resource and auth import shell, and none of them may
// import another's types. resource.Strings is an alias of Strings.
//
// Strings reach the templates through the request context (With / From), so the
// exported shell.LoginPage and shell.MFAPage signatures did not have to change.
package i18n

import (
	"context"
	"reflect"
)

// Strings is every user-visible literal of the panel chrome.
//
// Text that embeds a value (a title, a count, an id) is a func rather than a
// format string: a wrong verb count in a format renders "%!s(MISSING)" without
// failing anything, and a func lets a language that declines nouns or counts
// by case put the value where its grammar wants it.
//
// The zero value of a field means "use the default"; there is deliberately no
// way to blank a string, because an empty button label is never intended.
type Strings struct {
	// Lang is the <html lang> attribute of every page the panel renders.
	Lang string

	// Sidebar chrome.
	Logout             string
	Settings           string
	ToggleSidebar      string
	OpenNavigation     string
	SwitchToLightTheme string // aria-label of the theme toggle while dark
	SwitchToDarkTheme  string // aria-label of the theme toggle while light

	// Login page.
	LoginTitle   string // <title>
	LoginHeading string // <h1>
	LoginUser    string // identifier label for single-user (HMAC) auth
	LoginEmail   string // identifier label for multi-user (Bcrypt) auth
	Password     string
	ShowPassword string
	HidePassword string
	SignIn       string

	// Login and second-factor errors.
	ErrInvalidUsernameOrPassword string
	ErrInvalidEmailOrPassword    string
	ErrTooManyLoginAttempts      string
	ErrTooManyCodeAttempts       string
	Err2FAUnavailable            string
	ErrInvalidSession            string
	ErrInvalidCode               string
	ErrCodeRequired              string

	// Second-factor challenge page.
	TwoFactorTitle string // <title>/<h1>, also the already-enabled page title
	TwoFactorCode  string // label of the code input
	Verify         string

	// Empty-state index (no resources registered).
	IndexHeading string
	IndexEmpty   string

	// List page.
	Total             func(n int) string
	New               string
	Edit              string
	Delete            string
	Undo              string
	DeleteRowLabel    func(id string) string      // aria-label of a row's Delete button
	Deleted           func(label string) string   // "<label> deleted" (toast and dimmed row)
	SearchPlaceholder func(caption string) string // text-search filter placeholder
	ViewsLabel        string                      // caption beside the view chips
	NoResults         string
	Prev              string
	Next              string
	LoadMore          string

	// Form pages.
	EditTitle           func(title string) string
	NewTitle            func(title string) string
	BackToList          string
	SaveChanges         string
	Create              string
	Cancel              string
	LocaleSwitcherLabel string
	LocaleEditingNote   func(active, def string) string
	SelectPlaceholder   string

	// Field validation messages; label is the field's own label.
	FieldRequired    func(label string) string
	FieldNotNumber   func(label string) string
	FieldNotDate     func(label string) string
	FieldNotDateTime func(label string) string
	FieldNotChoice   func(label string) string
	FieldNotJSON     func(label string) string

	// Detail page.
	BackTo        func(title string) string
	NoDetail      string
	ConfirmDelete func(title string) string

	// Trash.
	TrashTitle      string // page heading and sidebar entry
	TrashIntro      string
	TrashEmpty      string
	TrashUnreadable string
	TrashShowing    func(shown, total int) string
	RestoreRowLabel func(id string) string // aria-label of a trash row's restore button
	SystemGroup     string                 // sidebar heading the Trash entry sits under

	// Two-factor enrollment (resource.MountTOTPEnrollment).
	TOTPSetupTitle        string
	TOTPScanPrompt        string
	TOTPQRAlt             string
	TOTPManualEntry       string
	TOTPURIPrompt         string
	TOTPCodeLabel         string
	ConfirmPassword       string
	TOTPVerifyEnable      string
	TOTPAlreadyEnabled    string
	TOTPDisable           string
	BackToDashboard       string
	RecoverySave          string
	RecoveryNote          string
	TOTPEnabledTitle      string
	TOTPEnabledBody       string
	ContinueToDashboard   string
	TOTPDisabledTitle     string
	TOTPDisabledBody      string
	RecoveryRegenTitle    string
	RecoveryRegenBody     string
	Continue              string
	TOTPDisableTitle      string
	RecoveryRegenerate    string // title of the regenerate re-auth page
	RecoveryRegenerateBtn string
	ErrIncorrectPassword  string
	ErrTOTPInvalidCode    string
}

// WithDefaults returns s with every unset field (empty string, nil func)
// replaced by the matching Default field. It is idempotent.
//
// Reflection rather than a hand-written field list: a hand-written list is
// exactly the thing that silently omits the field added next release, leaving
// it empty in every merged value. The test that sets every field to a marker
// proves nothing is skipped here.
func (s Strings) WithDefaults() Strings {
	def := reflect.ValueOf(Default())
	out := reflect.ValueOf(&s).Elem()
	for i := range out.NumField() {
		f := out.Field(i)
		switch f.Kind() { //nolint:exhaustive // only string and func fields exist; Strings' tests assert that
		case reflect.String:
			if f.String() == "" {
				f.Set(def.Field(i))
			}
		case reflect.Func:
			if f.IsNil() {
				f.Set(def.Field(i))
			}
		}
	}
	return s
}

type ctxKey struct{}

// With stores s (merged over Default) on ctx. The panel does this once per
// request, so every template, handler and authenticator renders from the same
// value.
func With(ctx context.Context, s Strings) context.Context {
	return context.WithValue(ctx, ctxKey{}, s.WithDefaults())
}

// WithMerged is With for a value that already went through WithDefaults. It
// exists so the per-request path does not repeat a reflective merge that
// cannot change.
func WithMerged(ctx context.Context, s Strings) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

var defaults = Default()

// From returns the strings on ctx, or Default when the context did not come
// from a panel request (a template rendered directly in a test, a consumer's
// own route that never passed through the panel).
func From(ctx context.Context) Strings {
	if s, ok := ctx.Value(ctxKey{}).(Strings); ok {
		return s
	}
	return defaults
}
