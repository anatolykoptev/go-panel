// Package session defines the public-auth session model and its Redis-backed
// store. A session is an opaque, server-side record keyed by a 256-bit
// crypto/rand id; the id is the only secret handed to the client (in the
// pn_pub cookie). All session state lives in Redis so the SSR render path reads
// it with a single HGETALL and never HTTP-calls go-grad (the F1 invariant).
package session

import (
	"context"
	"errors"
	"time"
)

// ErrSessionNotFound is returned by Get/Rotate when no live session exists for
// the given id (missing, revoked, or TTL-expired — all indistinguishable to the
// caller by design).
var ErrSessionNotFound = errors.New("identity/session: session not found")

// OrgRef is a denormalized org-membership entry carried in a session snapshot so
// the render path needs no extra query. Empty until the advertiser cabinet (NG2).
type OrgRef struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// UserSnapshot is the session payload: a point-in-time copy of the user's
// identity and memberships. Rev lets consumers detect a stale snapshot after a
// membership change (the user.rev column is bumped server-side).
type UserSnapshot struct {
	UserID      string
	DisplayName string
	CitySlug    string
	Orgs        []OrgRef
	Rev         int
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

// SessionStore persists UserSnapshots behind opaque session ids.
type SessionStore interface {
	// Create stores snap under a fresh crypto-random sid with the given TTL and
	// returns the sid.
	Create(ctx context.Context, snap UserSnapshot, ttl time.Duration) (sid string, err error)
	// Get returns the snapshot for sid, or ErrSessionNotFound.
	Get(ctx context.Context, sid string) (UserSnapshot, error)
	// Revoke deletes the session for sid. Idempotent: revoking a missing session
	// is not an error.
	Revoke(ctx context.Context, sid string) error
	// RevokeAllForUser deletes every live session for userID (logout-everywhere).
	RevokeAllForUser(ctx context.Context, userID string) error
	// Rotate mints a new sid carrying the existing snapshot (with its remaining
	// lifetime) and destroys the old sid. Session-fixation defense.
	Rotate(ctx context.Context, oldSID string) (newSID string, err error)
}
