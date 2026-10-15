package i18n

import "strconv"

// english is the English strings — the exact text go-panel rendered before
// the chrome was configurable. Changing a value here changes the page for every
// consumer that never set Strings, so the golden test in package resource pins
// each one.
//
// Undo is Russian by history: the Trash and the delete-undo toast have always
// said «Вернуть», and a panel that configured nothing must not change.
// (A package-level value rather than a function body: the literal is long, and
// Strings holds only strings and funcs, so a copy cannot alias anything mutable.)
//
//nolint:gosec // G101 false positive: UI copy that mentions "password", not a credential
var english = Strings{
	Lang: "en",

	Logout:             "Logout",
	Settings:           "Settings",
	ToggleSidebar:      "Toggle sidebar",
	OpenNavigation:     "Open navigation",
	SwitchToLightTheme: "Switch to light theme",
	SwitchToDarkTheme:  "Switch to dark theme",

	LoginTitle:   "Admin Login",
	LoginHeading: "Admin",
	LoginUser:    "Username",
	LoginEmail:   "Email",
	Password:     "Password",
	ShowPassword: "Show password",
	HidePassword: "Hide password",
	SignIn:       "Sign in",

	ErrInvalidUsernameOrPassword: "Invalid username or password",
	ErrInvalidEmailOrPassword:    "Invalid email or password",
	ErrTooManyLoginAttempts:      "Too many login attempts. Please try again later.",
	ErrTooManyCodeAttempts:       "Too many verification attempts. Please try again later.",
	Err2FAUnavailable:            "Two-factor authentication is unavailable",
	ErrInvalidSession:            "Invalid session or account",
	ErrInvalidCode:               "Invalid code or recovery code",
	ErrCodeRequired:              "Enter a verification code or recovery code",

	TwoFactorTitle: "Two-Factor Authentication",
	TwoFactorCode:  "Authentication code or recovery code",
	Verify:         "Verify",

	IndexHeading: "Admin",
	IndexEmpty:   "No resources registered yet.",

	Total:             func(n int) string { return strconv.Itoa(n) + " total" },
	New:               "+ New",
	Edit:              "Edit",
	Delete:            "Delete",
	Undo:              "Вернуть",
	DeleteRowLabel:    func(id string) string { return "Delete " + id },
	Deleted:           func(label string) string { return label + " deleted" },
	SearchPlaceholder: func(caption string) string { return "Search " + caption + "..." },
	ViewsLabel:        "view",
	NoResults:         "No results",
	Prev:              "Prev",
	Next:              "Next",
	LoadMore:          "Load more",

	EditTitle:           func(title string) string { return "Edit " + title },
	NewTitle:            func(title string) string { return "New " + title },
	BackToList:          "Back to list",
	SaveChanges:         "Save Changes",
	Create:              "Create",
	Cancel:              "Cancel",
	LocaleSwitcherLabel: "Locale",
	LocaleEditingNote: func(active, def string) string {
		return "Editing the " + active + " translation. Shared fields are edited on the " + def + " version."
	},
	SelectPlaceholder: "— select —",

	FieldRequired:    func(label string) string { return label + " is required" },
	FieldNotNumber:   func(label string) string { return label + " must be a number" },
	FieldNotDate:     func(label string) string { return label + " must be a valid date (YYYY-MM-DD)" },
	FieldNotDateTime: func(label string) string { return label + " must be a valid date and time (YYYY-MM-DD HH:MM)" },
	FieldNotChoice:   func(label string) string { return label + " is not a valid choice" },
	FieldNotJSON:     func(label string) string { return label + " must be valid JSON" },

	BackTo:   func(title string) string { return "Back to " + title },
	NoDetail: "No detail available.",
	ConfirmDelete: func(title string) string {
		return "Delete this " + title + "? This cannot be undone."
	},

	TrashTitle:      "Trash",
	TrashIntro:      "Deleted rows are kept, never destroyed. Restore one here at any time.",
	TrashEmpty:      "Trash is empty.",
	TrashUnreadable: "Could not read this resource's trash. The rows are still there — see the server log.",
	TrashShowing: func(shown, total int) string {
		return "Showing the " + strconv.Itoa(shown) + " most recently deleted of " + strconv.Itoa(total) + "."
	},
	RestoreRowLabel: func(id string) string { return "Restore " + id },
	SystemGroup:     "System",

	TOTPSetupTitle:        "Set Up Two-Factor Authentication",
	TOTPScanPrompt:        "Scan this code with your authenticator app (Google Authenticator, 1Password, Authy, …):",
	TOTPQRAlt:             "TOTP enrollment QR code -- scan with your authenticator app, or use the manual-entry code below",
	TOTPManualEntry:       "Can't scan? Enter this code manually:",
	TOTPURIPrompt:         "Full setup URI (some apps accept pasting this directly):",
	TOTPCodeLabel:         "Enter the 6-digit code from your app",
	ConfirmPassword:       "Confirm your current password",
	TOTPVerifyEnable:      "Verify & Enable",
	TOTPAlreadyEnabled:    "Two-factor authentication is already enabled on this account.",
	TOTPDisable:           "Disable 2FA",
	BackToDashboard:       "Back to dashboard",
	RecoverySave:          "Save these recovery codes now.",
	RecoveryNote:          "Each code works once, if you lose access to your authenticator app. They will not be shown again.",
	TOTPEnabledTitle:      "Two-Factor Authentication Enabled",
	TOTPEnabledBody:       "Your account is now protected with two-factor authentication.",
	ContinueToDashboard:   "Continue to dashboard",
	TOTPDisabledTitle:     "Two-Factor Authentication Disabled",
	TOTPDisabledBody:      "Two-factor authentication has been turned off for this account.",
	RecoveryRegenTitle:    "Recovery Codes Regenerated",
	RecoveryRegenBody:     "Your previous recovery codes no longer work.",
	Continue:              "Continue",
	TOTPDisableTitle:      "Disable Two-Factor Authentication",
	RecoveryRegenerate:    "Regenerate Recovery Codes",
	RecoveryRegenerateBtn: "Regenerate Codes",
	ErrIncorrectPassword:  "Incorrect password.",
	ErrTOTPInvalidCode:    "Invalid or already-used code. Enter the current code from your authenticator app.",
}

// Default returns the English strings; see english for the contract.
func Default() Strings { return english }
