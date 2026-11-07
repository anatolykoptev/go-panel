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

	SettingsNavLabel:       "Settings sections",
	SettingsGroupPersonal:  "Personal",
	SettingsGroupWorkspace: "Workspace",
	SettingsProfile:        "Profile",
	SettingsSecurity:       "Security",
	SettingsAppearance:     "Appearance",
	ProfileName:            "Name",
	ProfileEmail:           "Email",
	ProfileRole:            "Role",
	ProfileHint:            "Only an owner can change these details.",

	PasswordRow:        "Password",
	PasswordHint:       "Changing it signs you out on every other device.",
	ChangePassword:     "Change password",
	CurrentPassword:    "Current password",
	NewPassword:        "New password",
	RepeatPassword:     "Repeat new password",
	PasswordMinHint:    func(min int) string { return "At least " + strconv.Itoa(min) + " characters." },
	PasswordChanged:    "Password changed. Other devices have been signed out.",
	ErrPasswordCurrent: "The current password is not correct.",
	ErrPasswordShort:   func(min int) string { return "The new password must be at least " + strconv.Itoa(min) + " characters." },
	ErrPasswordLong: func(maxBytes int) string {
		return "The new password is too long (at most " + strconv.Itoa(maxBytes) + " bytes)."
	},
	ErrPasswordMismatch:    "The two new passwords do not match.",
	ErrPasswordUnchanged:   "The new password must differ from the current one.",
	ErrPasswordThrottled:   "Too many attempts. Please try again later.",
	ErrPasswordUnavailable: "The password could not be changed. Please try again.",
	TwoFactorRow:           "Two-factor sign-in",
	TwoFactorHint:          "A code from your phone in addition to the password.",
	TwoFactorOn:            "On",
	TwoFactorOff:           "Off",
	TwoFactorEnable:        "Turn on",
	SessionsRow:            "Sessions",
	SessionsHint:           "Sign out of devices you are signed in on elsewhere.",
	SignOutOthersBtn:       "Sign out on other devices",
	PasswordChangedSignIn:  "Password changed. Please sign in again.",
	SessionsDoneSignIn:     "Signed out on other devices. Please sign in again.",
	SessionsDone:           "Signed out on all other devices.",
	SignedInAs:             func(name string) string { return "You are signed in as " + name },

	Theme:     "Theme",
	ThemeHint: "How the panel looks. Saved for you and applied at once.",
	ThemeMode: "Mode",
	ModeHint:  "Also switchable with the sun and moon button in the menu.",
	ModeLight: "Light",
	ModeDark:  "Dark",

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
