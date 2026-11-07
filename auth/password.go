package auth

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

// MinPasswordLength is the shortest password ChangePassword accepts, in
// characters (runes). go-panel had no password policy before the settings page
// introduced self-service changes; 10 is the floor the owner asked for.
const MinPasswordLength = 10

// MaxPasswordBytes is bcrypt's input limit: longer passwords are silently
// truncated by older implementations and rejected by current ones, so a
// password over it is refused up front with a clear error rather than stored
// as something the operator cannot reproduce.
const MaxPasswordBytes = 72

var (
	// ErrPasswordTooShort: the new password is under MinPasswordLength characters.
	ErrPasswordTooShort = errors.New("auth: new password is too short")
	// ErrPasswordTooLong: the new password is over MaxPasswordBytes bytes.
	ErrPasswordTooLong = errors.New("auth: new password is too long")
	// ErrPasswordUnchanged: the new password equals the current one.
	ErrPasswordUnchanged = errors.New("auth: new password equals the current one")
)

// ValidateNewPassword applies the minimum policy. It returns one of
// ErrPasswordTooShort / ErrPasswordTooLong, never anything that contains the
// password.
func ValidateNewPassword(password string) error {
	switch {
	case utf8.RuneCountInString(password) < MinPasswordLength:
		return ErrPasswordTooShort
	case len(password) > MaxPasswordBytes:
		return ErrPasswordTooLong
	}
	return nil
}

// ChangePassword is the self-service password change: it re-proves the current
// password (bcrypt, constant-time within CompareHashAndPassword), applies the
// minimum policy, and stores the new bcrypt hash via AccountStore
// .UpdatePasswordHash. Order matters: the current password is verified FIRST,
// so a wrong one reveals nothing about the policy and changes nothing.
//
// Errors: ErrReauthFailed (wrong current password, or the account cannot be
// loaded; state untouched), ErrPasswordTooShort / ErrPasswordTooLong /
// ErrPasswordUnchanged (state untouched), or a wrapped store/hash error. No
// error contains a password or a hash.
//
// UpdatePasswordHash stamps the credential epoch (password_changed_at), which
// revokes every session issued before it, including the caller's own; the
// caller re-issues the operator's session (BcryptTOTPAuth.RefreshSession).
func ChangePassword(ctx context.Context, store AccountStore, acct *Account, current, next string) error {
	stored, err := store.GetByEmail(ctx, acct.Email)
	if err != nil {
		// Same result as a wrong password: no oracle for "account vanished".
		_ = VerifyPassword(current, dummyPasswordHash)
		return ErrReauthFailed
	}
	if !VerifyPassword(current, stored.PasswordHash) {
		return ErrReauthFailed
	}
	if err := ValidateNewPassword(next); err != nil {
		return err
	}
	if VerifyPassword(next, stored.PasswordHash) {
		return ErrPasswordUnchanged
	}
	hash, err := HashPassword(next)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}
	if err := store.UpdatePasswordHash(ctx, acct.ID, hash); err != nil {
		return fmt.Errorf("auth: store new password hash: %w", err)
	}
	return nil
}
