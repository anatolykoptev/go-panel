package identity

import (
	"context"
	"time"

	"github.com/anatolykoptev/go-panel/identity/session"
)

// UserStore is the persistence seam implemented by go-grad/identitystore over
// pgxpool. The framework depends only on this interface; it never imports go-grad.
//
// The provider UID is passed already hashed (HMAC-SHA256 with the per-region
// pepper, ADR-002) and serves as the identity-lookup key. The raw email is ALSO
// passed so the store can persist it as the user's contact address (operator
// decision: plaintext email, WordPress wp_users style — needed to email
// registered users later). The HMAC stays the lookup index; it does not replace
// storing the address.
type UserStore interface {
	// UpsertIdentity finds or creates the user+identity for (provider, uidHash),
	// recording email as the user's contact address (forwarded on every verify;
	// the store decides insert-only vs. update-on-login).
	// Returns the user id and whether a new user was created.
	UpsertIdentity(ctx context.Context, provider string, uidHash []byte, email string) (userID string, created bool, err error)
	// GetUserSnapshot returns the session snapshot (identity + memberships) for a
	// user, used to populate a new session.
	GetUserSnapshot(ctx context.Context, userID string) (session.UserSnapshot, error)
	// LinkDevice records an anonymous device (epid) → user mapping so favorites
	// keyed by epid merge into the authenticated account.
	LinkDevice(ctx context.Context, epid, userID string) error
}

// User mirrors the auth.users row. Email is the natural key; provider UIDs are
// stored hashed in a separate identities table (not modeled here — go-grad owns it).
type User struct {
	ID          string
	Email       string
	DisplayName string
	CitySlug    string
	Rev         int
	CreatedAt   time.Time
}

// Org mirrors the auth.orgs row (forward-compat for the advertiser cabinet, NG2).
type Org struct {
	ID       string
	Name     string
	CitySlug string
}

// Membership mirrors the auth.memberships row linking a user to an org with a role.
type Membership struct {
	UserID string
	OrgID  string
	Role   string
}
