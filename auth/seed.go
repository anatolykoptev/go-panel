package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidSeed is returned by SeedAccount for an empty password or an email
// that is not shaped like local@domain, or an empty (or whitespace-only) role.
var ErrInvalidSeed = errors.New("auth: invalid seed account input")

// SeedAccount idempotently creates the first (operator) account at boot.
//
// It rejects an empty password and an email that is not exactly one "@" with
// non-empty sides (ErrInvalidSeed), touching nothing. The email is lower-cased
// and trimmed first, matching what CreateAccount stores and the login path
// looks up.
//
// An account that already exists is left completely alone: its hash is never
// overwritten and it is never re-activated. So a password rotated in the
// database survives a restart that still carries the old env value, and an
// operator you deactivated stays deactivated.
//
// Existence is checked with GetByEmail, which only returns ACTIVE accounts. A
// deactivated account therefore reaches CreateAccount, whose ON CONFLICT DO
// NOTHING makes it a no-op: (id "", created false, nil). The id of a
// deactivated account is not recoverable through AccountStore.
//
// Role is not validated beyond being non-empty: there is no role registry
// (RoleOwner is the only constant), so unknown roles are stored as given.
//
// Returns the account id and whether this call created it. General contract:
// id may be EMPTY when created is false. Both a deactivated row and an active
// row with a NULL password hash miss GetByEmail and then hit ON CONFLICT DO
// NOTHING in CreateAccount. Callers must not use id unless created is true or
// the lookup branch returned it.
func SeedAccount(ctx context.Context, store AccountStore, email, password, name, role string) (id string, created bool, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if strings.TrimSpace(role) == "" {
		return "", false, fmt.Errorf("%w: empty role", ErrInvalidSeed)
	}
	if password == "" {
		return "", false, fmt.Errorf("%w: empty password", ErrInvalidSeed)
	}
	if !validSeedEmail(email) {
		return "", false, fmt.Errorf("%w: email %q is not local@domain", ErrInvalidSeed, email)
	}

	existing, err := store.GetByEmail(ctx, email)
	switch {
	case err == nil:
		return existing.ID, false, nil
	case !errors.Is(err, ErrAccountNotFound):
		return "", false, fmt.Errorf("auth: seed lookup: %w", err)
	}

	hash, err := HashPassword(password)
	if err != nil {
		return "", false, fmt.Errorf("auth: seed hash: %w", err)
	}
	id, created, err = store.CreateAccount(ctx, email, name, hash, role)
	if err != nil {
		return "", false, fmt.Errorf("auth: seed create: %w", err)
	}
	return id, created, nil
}

// validSeedEmail reports exactly one "@" with a non-empty local part and domain.
func validSeedEmail(email string) bool {
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@")
}
