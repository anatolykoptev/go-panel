package session_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/anatolykoptev/go-panel/identity/session"
)

const testTTL = 30 * time.Minute

func newStore(t *testing.T) (*session.RedisSessionStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return session.NewRedisSessionStore(rdb), mr
}

func sampleSnap() session.UserSnapshot {
	now := time.Now().UTC().Truncate(time.Second)
	return session.UserSnapshot{
		UserID:      "user-123",
		DisplayName: "Alice",
		CitySlug:    "spb",
		Orgs:        []session.OrgRef{{OrgID: "org-1", Name: "Acme", Role: "owner"}},
		Rev:         2,
		IssuedAt:    now,
		ExpiresAt:   now.Add(testTTL),
	}
}

func TestCreateAndGet(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	snap := sampleSnap()

	sid, err := st.Create(ctx, snap, testTTL)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sid == "" {
		t.Fatal("Create returned empty sid")
	}

	got, err := st.Get(ctx, sid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != snap.UserID || got.DisplayName != snap.DisplayName ||
		got.CitySlug != snap.CitySlug || got.Rev != snap.Rev {
		t.Fatalf("Get snapshot mismatch:\n got=%+v\nwant=%+v", got, snap)
	}
	if len(got.Orgs) != 1 || got.Orgs[0].OrgID != "org-1" || got.Orgs[0].Role != "owner" {
		t.Fatalf("Orgs not round-tripped: %+v", got.Orgs)
	}
	if !got.ExpiresAt.Equal(snap.ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, snap.ExpiresAt)
	}
}

// TestSIDIsCryptoRandom locks the property that session ids are 256-bit
// base64url tokens. Falsifiability: if sid were a counter or short string, the
// decoded length would not be 32 bytes and this fails.
func TestSIDIsCryptoRandom(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()

	sid1, _ := st.Create(ctx, sampleSnap(), testTTL)
	sid2, _ := st.Create(ctx, sampleSnap(), testTTL)
	if sid1 == sid2 {
		t.Fatal("two Create calls returned identical sids — not random")
	}
	raw, err := base64.RawURLEncoding.DecodeString(sid1)
	if err != nil {
		t.Fatalf("sid not base64url: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("sid decodes to %d bytes, want 32 (256-bit)", len(raw))
	}
}

func TestGetMissingReturnsErr(t *testing.T) {
	st, _ := newStore(t)
	_, err := st.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Get missing: err = %v, want ErrSessionNotFound", err)
	}
}

func TestRevoke(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	sid, _ := st.Create(ctx, sampleSnap(), testTTL)

	if err := st.Revoke(ctx, sid); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := st.Get(ctx, sid); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("after Revoke, Get err = %v, want ErrSessionNotFound", err)
	}
}

func TestRevokeAllForUser(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	snap := sampleSnap()

	sid1, _ := st.Create(ctx, snap, testTTL)
	sid2, _ := st.Create(ctx, snap, testTTL)

	if err := st.RevokeAllForUser(ctx, snap.UserID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	for _, sid := range []string{sid1, sid2} {
		if _, err := st.Get(ctx, sid); !errors.Is(err, session.ErrSessionNotFound) {
			t.Fatalf("session %s survived RevokeAllForUser: err=%v", sid, err)
		}
	}
}

// TestRotate locks session-fixation defense at the store layer: Rotate mints a
// new sid carrying the snapshot and destroys the old. Falsifiability: if Rotate
// returned the same id (no new id) or left the old readable, an assertion fails.
func TestRotate(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	snap := sampleSnap()
	oldSID, _ := st.Create(ctx, snap, testTTL)

	newSID, err := st.Rotate(ctx, oldSID)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newSID == oldSID {
		t.Fatal("Rotate returned same id — no fixation defense")
	}
	if _, err := st.Get(ctx, oldSID); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("old sid still valid after Rotate: %v", err)
	}
	got, err := st.Get(ctx, newSID)
	if err != nil {
		t.Fatalf("new sid not valid after Rotate: %v", err)
	}
	if got.UserID != snap.UserID {
		t.Fatalf("rotated snapshot UserID = %q, want %q", got.UserID, snap.UserID)
	}
}

func TestRotateMissingOldSID(t *testing.T) {
	st, _ := newStore(t)
	if _, err := st.Rotate(context.Background(), "ghost"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Rotate on missing sid err = %v, want ErrSessionNotFound", err)
	}
}

// TestTTLExpiry verifies the session key honours its TTL. Falsifiability: if
// Create did not set an expiry, the key would survive the fast-forward and this
// fails.
func TestTTLExpiry(t *testing.T) {
	st, mr := newStore(t)
	ctx := context.Background()
	sid, _ := st.Create(ctx, sampleSnap(), testTTL)

	mr.FastForward(testTTL + time.Minute)

	if _, err := st.Get(ctx, sid); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("session survived TTL expiry: err=%v", err)
	}
}
