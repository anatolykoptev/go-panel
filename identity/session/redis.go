package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key prefixes and hash field names. Defined as constants so the write
// path (Create) and read path (Get) can never drift.
const (
	sessPrefix = "pn_sess:"      // pn_sess:<sid> → hash of session fields
	userPrefix = "pn_sess_user:" // pn_sess_user:<uid> → set of active sids

	fUserID      = "user_id"
	fDisplayName = "display_name"
	fCitySlug    = "city_slug"
	fOrgs        = "orgs"
	fRev         = "rev"
	fIssuedAt    = "issued_at"
	fExpiresAt   = "expires_at"

	// sidBytes is the session-id entropy: 256 bits via crypto/rand.
	sidBytes = 32
)

// RedisSessionStore implements SessionStore over a go-redis client.
type RedisSessionStore struct {
	rdb redis.Cmdable
}

// NewRedisSessionStore wraps an existing go-redis client (or cluster client).
func NewRedisSessionStore(rdb redis.Cmdable) *RedisSessionStore {
	return &RedisSessionStore{rdb: rdb}
}

func sessKey(sid string) string { return sessPrefix + sid }
func userKey(uid string) string { return userPrefix + uid }

// newSID returns a 256-bit base64url session id from crypto/rand. math/rand is
// never used: a guessable sid is a session-hijack primitive.
func newSID() (string, error) {
	b := make([]byte, sidBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("identity/session: rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create stores snap under a fresh sid with the given TTL.
func (s *RedisSessionStore) Create(ctx context.Context, snap UserSnapshot, ttl time.Duration) (string, error) {
	sid, err := newSID()
	if err != nil {
		return "", err
	}

	orgs, err := json.Marshal(snap.Orgs)
	if err != nil {
		return "", fmt.Errorf("identity/session: marshal orgs: %w", err)
	}

	fields := map[string]any{
		fUserID:      snap.UserID,
		fDisplayName: snap.DisplayName,
		fCitySlug:    snap.CitySlug,
		fOrgs:        string(orgs),
		fRev:         strconv.Itoa(snap.Rev),
		fIssuedAt:    strconv.FormatInt(snap.IssuedAt.Unix(), 10),
		fExpiresAt:   strconv.FormatInt(snap.ExpiresAt.Unix(), 10),
	}

	sk, uk := sessKey(sid), userKey(snap.UserID)
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, sk, fields)
	pipe.PExpire(ctx, sk, ttl)
	pipe.SAdd(ctx, uk, sid)
	// Bound the index set's lifetime so a crashed Revoke can't leak it forever.
	pipe.PExpire(ctx, uk, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("identity/session: create: %w", err)
	}
	return sid, nil
}

// Get returns the snapshot for sid, or ErrSessionNotFound.
func (s *RedisSessionStore) Get(ctx context.Context, sid string) (UserSnapshot, error) {
	m, err := s.rdb.HGetAll(ctx, sessKey(sid)).Result()
	if err != nil {
		return UserSnapshot{}, fmt.Errorf("identity/session: hgetall: %w", err)
	}
	if len(m) == 0 {
		return UserSnapshot{}, ErrSessionNotFound
	}

	snap := UserSnapshot{
		UserID:      m[fUserID],
		DisplayName: m[fDisplayName],
		CitySlug:    m[fCitySlug],
	}
	if rev, err := strconv.Atoi(m[fRev]); err == nil {
		snap.Rev = rev
	}
	if ts, err := strconv.ParseInt(m[fIssuedAt], 10, 64); err == nil {
		snap.IssuedAt = time.Unix(ts, 0).UTC()
	}
	if ts, err := strconv.ParseInt(m[fExpiresAt], 10, 64); err == nil {
		snap.ExpiresAt = time.Unix(ts, 0).UTC()
	}
	if raw := m[fOrgs]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &snap.Orgs); err != nil {
			return UserSnapshot{}, fmt.Errorf("identity/session: unmarshal orgs: %w", err)
		}
	}
	return snap, nil
}

// Revoke deletes the session and removes it from the user's index set.
// Idempotent: a missing session is not an error.
func (s *RedisSessionStore) Revoke(ctx context.Context, sid string) error {
	// Learn the owning user so we can also prune the index set.
	uid, err := s.rdb.HGet(ctx, sessKey(sid), fUserID).Result()
	switch {
	case err == redis.Nil:
		// Session already gone; nothing to remove from any set.
		return nil
	case err != nil:
		return fmt.Errorf("identity/session: revoke lookup: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, sessKey(sid))
	pipe.SRem(ctx, userKey(uid), sid)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("identity/session: revoke: %w", err)
	}
	return nil
}

// RevokeAllForUser deletes every live session for userID.
func (s *RedisSessionStore) RevokeAllForUser(ctx context.Context, userID string) error {
	uk := userKey(userID)
	sids, err := s.rdb.SMembers(ctx, uk).Result()
	if err != nil {
		return fmt.Errorf("identity/session: smembers: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	for _, sid := range sids {
		pipe.Del(ctx, sessKey(sid))
	}
	pipe.Del(ctx, uk)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("identity/session: revoke-all: %w", err)
	}
	return nil
}

// Rotate mints a new sid for the existing snapshot and destroys the old one.
func (s *RedisSessionStore) Rotate(ctx context.Context, oldSID string) (string, error) {
	snap, err := s.Get(ctx, oldSID)
	if err != nil {
		return "", err // ErrSessionNotFound propagates
	}

	remaining := time.Until(snap.ExpiresAt)
	if remaining <= 0 {
		return "", ErrSessionNotFound
	}

	newSID, err := s.Create(ctx, snap, remaining)
	if err != nil {
		return "", err
	}
	if err := s.Revoke(ctx, oldSID); err != nil {
		return "", err
	}
	return newSID, nil
}
