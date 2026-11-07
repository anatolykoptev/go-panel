package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
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

// VerifyCurrentPassword re-proves the operator's current password against the
// stored hash (bcrypt, constant-time within CompareHashAndPassword). It returns
// ErrReauthFailed for a wrong password, an unknown account, or a stored row
// that is not acct's (the ID is asserted: a lookup by e-mail must land on the
// account the session names), and a wrapped error for a store failure, which is
// NOT a wrong password and must surface as an internal error.
func VerifyCurrentPassword(ctx context.Context, store AccountStore, acct *Account, current string) error {
	_, err := loadVerified(ctx, store, acct, current)
	return err
}

func loadVerified(ctx context.Context, store AccountStore, acct *Account, current string) (*Account, error) {
	stored, err := store.GetByEmail(ctx, acct.Email)
	switch {
	case errors.Is(err, ErrAccountNotFound), err == nil && (stored == nil || stored.ID != acct.ID):
		// Same cost and result as a wrong password: no oracle for "no such account".
		_ = VerifyPassword(current, dummyPasswordHash)
		return nil, ErrReauthFailed
	case err != nil:
		return nil, fmt.Errorf("auth: load account for password check: %w", err)
	}
	if !VerifyPassword(current, stored.PasswordHash) {
		return nil, ErrReauthFailed
	}
	return stored, nil
}

// ChangePassword is the self-service password change: it re-proves the current
// password, applies the minimum policy, and stores the new bcrypt hash through
// EpochStore.UpdatePasswordHashIfCurrent, which stamps the credential epoch
// atomically and only if the calling session (issued at sessionIatMs, from
// Session.IssuedAtMs) is still current. Order matters: the current password is
// verified FIRST, so a wrong one reveals nothing about the policy and changes
// nothing.
//
// It returns the epoch the database stamped; hand it to
// BcryptTOTPAuth.RefreshSession so the caller stays signed in while every other
// session stays revoked.
//
// Errors: ErrReauthFailed (wrong current password; state untouched),
// ErrPasswordTooShort / ErrPasswordTooLong / ErrPasswordUnchanged (state
// untouched), ErrSessionStale (the session was revoked meanwhile, or another
// change won the race; state untouched), or a wrapped store/hash error. No
// error contains a password or a hash.
func ChangePassword(ctx context.Context, store EpochAccountStore, acct *Account, sessionIatMs int64, current, next string) (time.Time, error) {
	stored, err := loadVerified(ctx, store, acct, current)
	if err != nil {
		return time.Time{}, err
	}
	if err := ValidateNewPassword(next); err != nil {
		return time.Time{}, err
	}
	if VerifyPassword(next, stored.PasswordHash) {
		return time.Time{}, ErrPasswordUnchanged
	}
	hash, err := HashPassword(next)
	if err != nil {
		return time.Time{}, fmt.Errorf("auth: hash new password: %w", err)
	}
	return store.UpdatePasswordHashIfCurrent(ctx, acct.ID, hash, sessionIatMs)
}

// RevokeOtherSessions is "sign out everywhere else": it re-proves the current
// password (as ChangePassword does), then stamps the epoch conditionally via
// EpochStore.RevokeSessionsIfCurrent. Same errors and the same returned epoch.
func RevokeOtherSessions(ctx context.Context, store EpochAccountStore, acct *Account, sessionIatMs int64, current string) (time.Time, error) {
	if _, err := loadVerified(ctx, store, acct, current); err != nil {
		return time.Time{}, err
	}
	return store.RevokeSessionsIfCurrent(ctx, acct.ID, sessionIatMs)
}
