package auth

import (
	"bytes"
	"fmt"
	"image/png"
	"net/url"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// DefaultTOTPSkew is the recommended +/-period tolerance for clock drift
// when validating a code (ValidateTOTPCode's skew parameter). skew=1
// accepts a code from one period (30s) before or after the current time —
// matching both pquerna/otp's own totp.Validate default and Google
// Authenticator's documented tolerance. Values above 1 widen the guessable
// window and are "likely sketchy" per pquerna/otp's own doc comment.
const DefaultTOTPSkew uint = 1

// totpPeriod is the RFC 6238 rotation period in seconds. Left at the
// library default (30s) for maximum authenticator-app compatibility —
// TOTPStepAt derives the replay-guard step from this SAME constant so the
// two can never drift apart.
const totpPeriod = 30

// GenerateTOTPSecret mints a new RFC 6238 TOTP key for accountName under
// issuer, using pquerna/otp's library defaults (SHA1, 6 digits, 30s
// period, 160-bit secret). Defaults are intentionally NOT overridden: every
// mainstream authenticator app (Google Authenticator, Authy, 1Password,
// Microsoft Authenticator, ...) assumes them, and deviating risks an app
// silently computing the wrong code with no error surfaced to the user.
//
// issuer and accountName are both required — pquerna/otp's Generate
// returns an error otherwise, since they're what the authenticator app
// displays to identify the account (e.g. "Issuer: AccountName" in the
// app's list).
//
// The returned Key's Secret() is the base32 plaintext to pass to
// EncryptTOTPSecret for storage. Its Image()/URL() are for one-time
// enrollment display (QR + otpauth:// text fallback) — see GenerateQRPNG.
// GenerateTOTPSecret must be called exactly once per enrollment attempt;
// re-displaying the QR for an already-pending secret goes through
// RebuildTOTPKey instead, never a second call to this function (which
// would mint a DIFFERENT secret and silently orphan the first).
func GenerateTOTPSecret(issuer, accountName string) (*otp.Key, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: generate totp secret: %w", err)
	}
	return key, nil
}

// RebuildTOTPKey reconstructs the *otp.Key for an already-minted secret —
// e.g. to re-render the QR code (GenerateQRPNG) on a reloaded enrollment
// page without minting a NEW secret and orphaning the one already written
// to the store. secret is the base32 string as returned by
// GenerateTOTPSecret's Key.Secret(): the decrypted, currently-pending
// stored secret. Never pass a freshly generated secret here — that is
// GenerateTOTPSecret's job.
func RebuildTOTPKey(issuer, accountName, secret string) (*otp.Key, error) {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", otp.AlgorithmSHA1.String())
	v.Set("digits", otp.DigitsSix.String())
	v.Set("period", "30")
	uri := fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(issuer+":"+accountName), v.Encode())
	key, err := otp.NewKeyFromURL(uri)
	if err != nil {
		return nil, fmt.Errorf("auth: rebuild totp key: %w", err)
	}
	return key, nil
}

// GenerateQRPNG renders key's otpauth:// URI as a PNG QR code of size
// width x height pixels, suitable for an <img> enrollment display. The
// otpauth:// URI text itself (key.URL()) is the guaranteed manual-entry
// fallback and has no CSP dependency; a same-origin PNG needs img-src
// 'self'.
func GenerateQRPNG(key *otp.Key, width, height int) ([]byte, error) {
	img, err := key.Image(width, height)
	if err != nil {
		return nil, fmt.Errorf("auth: generate totp qr image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("auth: encode totp qr png: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateTOTPCode reports whether code is a valid RFC 6238 TOTP code for
// secret (base32, as returned by GenerateTOTPSecret's Key.Secret()) at the
// current time, allowing +/-skew periods (30s each) of clock drift.
// Comparison is constant-time: pquerna/otp's hotp.ValidateCustom compares
// the computed and submitted codes via crypto/subtle.ConstantTimeCompare
// (verified against the pquerna/otp v1.5.0 source, hotp/hotp.go).
//
// This checks CODE VALIDITY ONLY — it does not consult or advance the
// replay guard (TOTPStore.ConsumeTOTPStep). A caller MUST also call
// ConsumeTOTPStep (keyed by TOTPStepAt(time.Now())) on a true result
// before treating the login/confirm as authoritative, or the same code
// stays usable for the rest of its validity window.
func ValidateTOTPCode(secret, code string, skew uint) bool {
	return ValidateTOTPCodeAt(secret, code, skew, time.Now())
}

// ValidateTOTPCodeAt is ValidateTOTPCode with an explicit time, for
// deterministic tests (RFC 6238 known-answer vectors, skew-window
// boundaries) and any future caller needing to validate against a
// non-"now" instant.
func ValidateTOTPCodeAt(secret, code string, skew uint, t time.Time) bool {
	valid, err := totp.ValidateCustom(code, secret, t, totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      skew,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false // fail closed: malformed code/secret is not a valid match
	}
	return valid
}

// TOTPStepAt returns the RFC 6238 time-step counter for t at the standard
// 30-second period — the value TOTPStore.ConsumeTOTPStep's replay guard
// persists and compares. Exported so a caller derives the step from the
// SAME period this package validates against (totpPeriod), rather than
// re-deriving floor(unix/30) itself and risking drift between the two.
func TOTPStepAt(t time.Time) int64 {
	return t.Unix() / totpPeriod
}
